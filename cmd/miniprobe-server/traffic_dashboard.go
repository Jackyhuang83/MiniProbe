package main

import (
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The traffic dashboard is read-only. It does not modify Agent billing,
// protection policy, alerts, or the main MiniProbe database.
const (
	trafficDashboardMaxDays = 90
	trafficDashboardMaxGap  = 2 * time.Hour
)

type dashboardTrafficTotals struct {
	InboundBytes  uint64 `json:"inbound_bytes"`
	OutboundBytes uint64 `json:"outbound_bytes"`
	UsedBytes     uint64 `json:"used_bytes"`
}

type dashboardTrafficDay struct {
	Date        string `json:"date"`
	dashboardTrafficTotals
	Incomplete bool `json:"incomplete"`
}

type dashboardTrafficNode struct {
	ID              string                 `json:"id"`
	DisplayName     string                 `json:"display_name"`
	Online          bool                   `json:"online"`
	LastSeen        time.Time              `json:"last_seen,omitempty"`
	History         dashboardTrafficTotals `json:"history"`
	HistoryPoints   int                    `json:"history_points"`
	Incomplete      bool                   `json:"incomplete"`
	QuotaBytes      uint64                 `json:"quota_bytes"`
	CycleUsedBytes  uint64                 `json:"cycle_used_bytes"`
	RemainingBytes  uint64                 `json:"remaining_bytes"`
	UsagePercent    float64                `json:"usage_percent"`
	Direction       string                 `json:"direction"`
	CycleStart      time.Time              `json:"cycle_start,omitempty"`
}

type dashboardTrafficResult struct {
	From           string                 `json:"from"`
	To             string                 `json:"to"`
	TZOffset       int                    `json:"tz_offset_minutes"`
	RetentionDays  int                    `json:"retention_days"`
	BucketMinutes  int                    `json:"bucket_minutes"`
	Totals         dashboardTrafficTotals `json:"totals"`
	Days           []dashboardTrafficDay  `json:"days"`
	Nodes          []dashboardTrafficNode `json:"nodes"`
	Incomplete     bool                   `json:"incomplete"`
	Method         string                 `json:"method"`
}

func trafficDashboardDates(query string, now time.Time, offset int) (string, string, time.Time, time.Time, error) {
	zone := time.FixedZone("viewer", offset*60)
	localNow := now.In(zone)
	defaultTo := localNow.Format("2006-01-02")
	defaultFrom := localNow.AddDate(0, 0, -29).Format("2006-01-02")
	values := strings.Split(query, "|")
	if len(values) != 2 {
		return "", "", time.Time{}, time.Time{}, errors.New("invalid date range")
	}
	from, to := values[0], values[1]
	if from == "" && to == "" {
		from, to = defaultFrom, defaultTo
	}
	a, errA := time.ParseInLocation("2006-01-02", from, zone)
	b, errB := time.ParseInLocation("2006-01-02", to, zone)
	if errA != nil || errB != nil || a.Format("2006-01-02") != from || b.Format("2006-01-02") != to {
		return "", "", time.Time{}, time.Time{}, errors.New("dates must be YYYY-MM-DD")
	}
	count := int(b.Sub(a).Hours()/24) + 1
	if count < 1 || count > trafficDashboardMaxDays || a.After(localNow) {
		return "", "", time.Time{}, time.Time{}, errors.New("date range must be 1..90 days, not in the future")
	}
	return from, to, a.UTC(), b.AddDate(0, 0, 1).UTC(), nil
}

func saturatingTrafficSum(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}

func addTrafficTotals(a *dashboardTrafficTotals, b dashboardTrafficTotals) {
	a.InboundBytes = saturatingTrafficSum(a.InboundBytes, b.InboundBytes)
	a.OutboundBytes = saturatingTrafficSum(a.OutboundBytes, b.OutboundBytes)
	a.UsedBytes = saturatingTrafficSum(a.UsedBytes, b.UsedBytes)
}

func trafficSnapshotValidPair(prev, cur trafficHistoryCheckpoint) bool {
	if prev.CycleStart != cur.CycleStart || cur.At <= prev.At || cur.At-prev.At > int64(trafficDashboardMaxGap/time.Second) {
		return false
	}
	// Counter regression (e.g. bad Agent snapshot) is never interpreted as usage.
	if cur.InboundBytes < prev.InboundBytes || cur.OutboundBytes < prev.OutboundBytes || cur.UsedBytes < prev.UsedBytes {
		return false
	}
	// If the billing policy changed within a cycle, billed deltas are ambiguous.
	return prev.Direction == cur.Direction && prev.LimitBytes == cur.LimitBytes &&
		prev.ResetDay == cur.ResetDay && prev.ResetTZMinutes == cur.ResetTZMinutes
}

// aggregateTrafficNode attributes each sufficiently short sampled interval to
// its ending checkpoint's viewer-local date. It never distributes an offline
// multi-day delta across days, never counts the first checkpoint as usage, and
// never joins two different billing cycles.
func aggregateTrafficNode(records []trafficHistoryCheckpoint, start, end time.Time, zone *time.Location, days map[string]*dashboardTrafficDay) (dashboardTrafficTotals, int, bool) {
	var total dashboardTrafficTotals
	points := 0
	incomplete := false
	lastByCycle := make(map[int64]trafficHistoryCheckpoint)
	for _, cur := range records {
		at := time.Unix(cur.At, 0).UTC()
		if at.After(end) || at.Equal(end) {
			break
		}
		prev, exists := lastByCycle[cur.CycleStart]
		lastByCycle[cur.CycleStart] = cur
		if at.Before(start) {
			continue
		}
		points++
		day := days[at.In(zone).Format("2006-01-02")]
		if day == nil {
			continue
		}
		if !exists || !trafficSnapshotValidPair(prev, cur) {
			incomplete = true
			day.Incomplete = true
			continue
		}
		delta := dashboardTrafficTotals{
			InboundBytes:  cur.InboundBytes - prev.InboundBytes,
			OutboundBytes: cur.OutboundBytes - prev.OutboundBytes,
			UsedBytes:     cur.UsedBytes - prev.UsedBytes,
		}
		addTrafficTotals(&day.dashboardTrafficTotals, delta)
		addTrafficTotals(&total, delta)
	}
	return total, points, incomplete
}

func (s *server) trafficDashboardAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := r.URL.Query()
	offset := 0
	if raw := query.Get("tz"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < -840 || n > 840 {
			http.Error(w, "invalid timezone", http.StatusBadRequest)
			return
		}
		offset = n
	}
	now := time.Now().UTC()
	from, to, start, end, err := trafficDashboardDates(query.Get("from")+"|"+query.Get("to"), now, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	zone := time.FixedZone("viewer", offset*60)
	days := make(map[string]*dashboardTrafficDay)
	ordered := make([]*dashboardTrafficDay, 0, trafficDashboardMaxDays)
	for d := start.In(zone); d.Before(end.In(zone)); d = d.AddDate(0, 0, 1) {
		day := &dashboardTrafficDay{Date: d.Format("2006-01-02")}
		days[day.Date] = day
		ordered = append(ordered, day)
	}
	// Snapshot live state quickly; history I/O never runs under the main lock.
	s.mu.RLock()
	type snapshot struct {
		id string
		config nodeConfig
		state commonNodeViewTraffic
	}
	nodes := make([]snapshot, 0, len(s.db.Nodes))
	for id, config := range s.db.Nodes {
		v := s.db.States[id]
		nodes = append(nodes, snapshot{id: id, config: config, state: commonNodeViewTraffic{
			LastSeen: v.LastSeen, Traffic: v.Traffic,
		}})
	}
	s.mu.RUnlock()

	result := dashboardTrafficResult{
		From: from, To: to, TZOffset: offset, RetentionDays: trafficHistoryRetentionDays,
		BucketMinutes: int(trafficHistoryInterval / time.Minute),
		Days: make([]dashboardTrafficDay, 0, len(ordered)),
		Nodes: make([]dashboardTrafficNode, 0, len(nodes)),
		Method: "sampled-cycle-deltas",
	}
	// A prior checkpoint is required to establish a baseline at range start.
	// This allows intervals crossing midnight to count without manufacturing
	// usage from the initial monthly cumulative counter.
	baselineFrom := start.Add(-24 * time.Hour)
	scanUntil := end
	if scanUntil.After(now) {
		scanUntil = now
	}
	for _, node := range nodes {
		history, readErr := s.readTrafficHistory(node.id, baselineFrom, scanUntil)
		if readErr != nil {
			http.Error(w, "traffic history unavailable", http.StatusInternalServerError)
			return
		}
		historyTotals, points, incomplete := aggregateTrafficNode(history, start, end, zone, days)
		quota := node.config.MonthlyTrafficLimit
		used := node.state.Traffic.UsedBytes
		remaining := uint64(0)
		if quota > used {
			remaining = quota - used
		}
		percent := float64(0)
		if quota > 0 {
			percent = 100 * float64(used) / float64(quota)
		}
		result.Nodes = append(result.Nodes, dashboardTrafficNode{
			ID: node.id, DisplayName: node.config.DisplayName,
			Online: !node.state.LastSeen.IsZero() && now.Sub(node.state.LastSeen) < 20*time.Second,
			LastSeen: node.state.LastSeen,
			History: historyTotals, HistoryPoints: points, Incomplete: incomplete,
			QuotaBytes: quota, CycleUsedBytes: used, RemainingBytes: remaining,
			UsagePercent: percent, Direction: node.config.TrafficDirection,
			CycleStart: node.state.Traffic.CycleStart,
		})
		if incomplete {
			result.Incomplete = true
		}
		addTrafficTotals(&result.Totals, historyTotals)
	}
	sort.Slice(result.Nodes, func(i, j int) bool {
		if result.Nodes[i].History.UsedBytes != result.Nodes[j].History.UsedBytes {
			return result.Nodes[i].History.UsedBytes > result.Nodes[j].History.UsedBytes
		}
		return result.Nodes[i].DisplayName < result.Nodes[j].DisplayName
	})
	for _, day := range ordered {
		result.Days = append(result.Days, *day)
	}
	noStore(w)
	writeJSON(w, http.StatusOK, result)
}

// Kept intentionally small: a copy of just the live fields required by this
// read-only endpoint, decoupled from unrelated network and host telemetry.
type commonNodeViewTraffic struct {
	LastSeen time.Time
	Traffic  trafficSnapshotView
}

type trafficSnapshotView struct {
	UsedBytes  uint64
	CycleStart time.Time
}
