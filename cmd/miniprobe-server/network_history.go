package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

const (
	networkHistoryRetentionDays = 31
	networkHistoryHardLimit     = int64(256 << 20)
	routeChangeConfirmations    = 2
	routeSnapshotInterval       = 30 * time.Minute
)

type historyMinuteProbe struct {
	Name       string
	Target     string
	LatencySum float64
	LatencyN   int
	Sent       int
	Lost       int
}

type historyMinuteAccumulator struct {
	Minute   int64
	Protocol string
	Probes   map[string]*historyMinuteProbe
}

type historyDiskProbe struct {
	Name      string  `json:"n"`
	Target    string  `json:"g"`
	LatencyMS float64 `json:"r"`
	Sent      int     `json:"s"`
	Lost      int     `json:"l"`
}

type historyDiskRecord struct {
	At       int64              `json:"t"`
	Protocol string             `json:"p,omitempty"`
	Probes   []historyDiskProbe `json:"x"`
}

type historyPoint struct {
	At        int64   `json:"at"`
	LatencyMS float64 `json:"latency_ms"`
	Sent      int     `json:"sent"`
	Lost      int     `json:"lost"`
	LossPct   float64 `json:"loss_pct"`
}

type historySeries struct {
	Name             string         `json:"name"`
	Target           string         `json:"target"`
	CurrentLatencyMS float64        `json:"current_latency_ms"`
	Sent             int            `json:"sent"`
	Lost             int            `json:"lost"`
	LossPct          float64        `json:"loss_pct"`
	Points           []historyPoint `json:"points"`
}

type historyAgg struct {
	LatencySum float64
	LatencyN   int
	Sent       int
	Lost       int
}

type routeHopState struct {
	Hop int    `json:"hop"`
	ASN string `json:"asn,omitempty"`
}

type routeCarrierState struct {
	Name           string          `json:"name"`
	Target         string          `json:"target"`
	Available      bool            `json:"available"`
	Baseline       []string        `json:"baseline,omitempty"`
	Current        []string        `json:"current,omitempty"`
	Status         string          `json:"status"`
	FirstDifferent int             `json:"first_different,omitempty"`
	LastChecked    time.Time       `json:"last_checked,omitempty"`
	ChangedAt      time.Time       `json:"changed_at,omitempty"`
	Candidate      []string        `json:"candidate,omitempty"`
	CandidateCount int             `json:"candidate_count,omitempty"`
	Hops           []routeHopState `json:"hops,omitempty"`
}

type routeChangeEvent struct {
	At   time.Time `json:"at"`
	Name string    `json:"name"`
	Type string    `json:"type"` // changed, recovered
	From []string  `json:"from,omitempty"`
	To   []string  `json:"to,omitempty"`
}

type routeNodeState struct {
	Version     int                          `json:"version"`
	NodeID      string                       `json:"node_id"`
	LastChecked time.Time                    `json:"last_checked,omitempty"`
	Carriers    map[string]routeCarrierState `json:"carriers"`
	Events      []routeChangeEvent           `json:"events,omitempty"`
}

type routeJob struct {
	NodeID string
	At     time.Time
	Routes []common.RouteResult
}

type asnCacheEntry struct {
	ASN     string
	Expires time.Time
}

type publicRouteHop struct {
	Hop int    `json:"hop"`
	ASN string `json:"asn,omitempty"`
}

type publicRouteCarrier struct {
	Name           string           `json:"name"`
	Target         string           `json:"target"`
	Available      bool             `json:"available"`
	Baseline       []string         `json:"baseline,omitempty"`
	Current        []string         `json:"current,omitempty"`
	Status         string           `json:"status"`
	FirstDifferent int              `json:"first_different,omitempty"`
	LastChecked    time.Time        `json:"last_checked,omitempty"`
	ChangedAt      *time.Time       `json:"changed_at,omitempty"`
	Hops           []publicRouteHop `json:"hops,omitempty"`
}

type publicRouteEvent struct {
	At   time.Time `json:"at"`
	Name string    `json:"name"`
	Type string    `json:"type"`
	From []string  `json:"from,omitempty"`
	To   []string  `json:"to,omitempty"`
}

type publicRouteNode struct {
	Status       string               `json:"status"`
	ChangedCount int                  `json:"changed_count"`
	LastChecked  time.Time            `json:"last_checked,omitempty"`
	Carriers     []publicRouteCarrier `json:"carriers"`
	Events       []publicRouteEvent   `json:"events,omitempty"`
	ASNSource    string               `json:"asn_source"`
}

func (s *server) initNetworkHistory() {
	if s.historyDir == "" {
		s.historyDir = filepath.Join(filepath.Dir(s.dataFile), "network-history")
	}
	_ = os.MkdirAll(s.historyDir, 0750)
	if s.historyAcc == nil {
		s.historyAcc = map[string]*historyMinuteAccumulator{}
	}
	if s.historyLastProbe == nil {
		s.historyLastProbe = map[string]time.Time{}
	}
	if s.routeStates == nil {
		s.routeStates = map[string]routeNodeState{}
	}
	if s.routeSeen == nil {
		s.routeSeen = map[string]time.Time{}
	}
	if s.asnCache == nil {
		s.asnCache = map[string]asnCacheEntry{}
	}
	s.loadRouteStates()
	_ = s.pruneNetworkHistory(time.Now().UTC())
}

func (s *server) ingestProbeSample(nodeID string, at time.Time, protocol string, probes []common.ProbeResult) {
	if at.IsZero() || len(probes) == 0 || !safeNodeID(nodeID) {
		return
	}
	rawAt := at.UTC()
	sampleAt := rawAt
	receivedAt := time.Now().UTC()
	if sampleAt.Before(receivedAt.Add(-5*time.Minute)) || sampleAt.After(receivedAt.Add(5*time.Minute)) {
		sampleAt = receivedAt
	}
	minute := sampleAt.Truncate(time.Minute).Unix()
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	if prev := s.historyLastProbe[nodeID]; !prev.IsZero() && !rawAt.After(prev) {
		return
	}
	s.historyLastProbe[nodeID] = rawAt
	acc := s.historyAcc[nodeID]
	if acc != nil && acc.Minute != minute {
		if err := s.flushHistoryAccumulatorLocked(nodeID, acc); err != nil {
			fmt.Fprintf(os.Stderr, "MiniProbe network history write failed: %v\n", err)
		}
		acc = nil
	}
	if acc == nil {
		acc = &historyMinuteAccumulator{Minute: minute, Protocol: protocol, Probes: map[string]*historyMinuteProbe{}}
		s.historyAcc[nodeID] = acc
	}
	if protocol != "" {
		acc.Protocol = protocol
	}
	for _, p := range probes {
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Target) == "" {
			continue
		}
		key := p.Name + "\x00" + p.Target
		x := acc.Probes[key]
		if x == nil {
			x = &historyMinuteProbe{Name: p.Name, Target: p.Target}
			acc.Probes[key] = x
		}
		sent := p.SampleSent
		lost := p.SampleLost
		if sent < 0 {
			sent = 0
		}
		if lost < 0 {
			lost = 0
		}
		if lost > sent {
			lost = sent
		}
		success := sent - lost
		if p.LatencyMS >= 0 {
			weight := success
			if weight <= 0 {
				weight = 1
			}
			x.LatencySum += p.LatencyMS * float64(weight)
			x.LatencyN += weight
		}
		x.Sent += sent
		x.Lost += lost
	}
}

func (s *server) networkHistoryLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	lastPrune := time.Now().UTC()
	for now := range t.C {
		s.flushCompletedHistoryMinutes(now.UTC())
		if now.Sub(lastPrune) >= 6*time.Hour {
			if err := s.pruneNetworkHistory(now.UTC()); err != nil {
				fmt.Fprintf(os.Stderr, "MiniProbe network history prune failed: %v\n", err)
			}
			lastPrune = now.UTC()
		}
	}
}

func (s *server) flushCompletedHistoryMinutes(now time.Time) {
	current := now.Truncate(time.Minute).Unix()
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	for nodeID, acc := range s.historyAcc {
		if acc == nil || acc.Minute >= current {
			continue
		}
		if err := s.flushHistoryAccumulatorLocked(nodeID, acc); err != nil {
			fmt.Fprintf(os.Stderr, "MiniProbe network history write failed: %v\n", err)
			continue
		}
		delete(s.historyAcc, nodeID)
	}
}

func (s *server) flushHistoryAccumulatorLocked(nodeID string, acc *historyMinuteAccumulator) error {
	if acc == nil || len(acc.Probes) == 0 || !safeNodeID(nodeID) {
		return nil
	}
	rec := historyDiskRecord{At: acc.Minute, Protocol: acc.Protocol}
	keys := make([]string, 0, len(acc.Probes))
	for key := range acc.Probes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		x := acc.Probes[key]
		lat := -1.0
		if x.LatencyN > 0 {
			lat = x.LatencySum / float64(x.LatencyN)
			lat = float64(int(lat*100+0.5)) / 100
		}
		rec.Probes = append(rec.Probes, historyDiskProbe{Name: x.Name, Target: x.Target, LatencyMS: lat, Sent: x.Sent, Lost: x.Lost})
	}
	return s.appendHistoryRecord(nodeID, rec)
}

func (s *server) appendHistoryRecord(nodeID string, rec historyDiskRecord) error {
	if !safeNodeID(nodeID) {
		return errors.New("invalid node id")
	}
	dir := filepath.Join(s.historyDir, nodeID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	day := time.Unix(rec.At, 0).UTC().Format("2006-01-02")
	path := filepath.Join(dir, day+".ndjson")
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *server) pruneNetworkHistory(now time.Time) error {
	if s.historyDir == "" {
		return nil
	}
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	cutoff := now.UTC().AddDate(0, 0, -networkHistoryRetentionDays).Format("2006-01-02")
	type entry struct {
		path string
		mod  time.Time
		size int64
	}
	var files []entry
	var total int64
	_ = filepath.WalkDir(s.historyDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".ndjson") {
			return nil
		}
		day := strings.TrimSuffix(d.Name(), ".ndjson")
		if len(day) == 10 && day < cutoff {
			_ = os.Remove(path)
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, entry{path: path, mod: st.ModTime(), size: st.Size()})
		total += st.Size()
		return nil
	})
	if total <= networkHistoryHardLimit {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	target := networkHistoryHardLimit * 9 / 10
	for _, f := range files {
		if total <= target {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
	return nil
}

func safeNodeID(id string) bool {
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func historyRange(raw string, now time.Time) (string, time.Time, int64) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "week":
		return "week", now.Add(-7 * 24 * time.Hour), 5 * 60
	case "month":
		return "month", now.Add(-30 * 24 * time.Hour), 30 * 60
	default:
		return "day", now.Add(-24 * time.Hour), 60
	}
}

func (s *server) networkHistoryAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nodeID := strings.TrimSpace(r.URL.Query().Get("id"))
	if !safeNodeID(nodeID) {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	cfg, ok := s.db.Nodes[nodeID]
	state := s.db.States[nodeID]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	now := time.Now().UTC()
	s.flushCompletedHistoryMinutes(now)
	rangeName, since, bucket := historyRange(r.URL.Query().Get("range"), now)
	series, err := s.readHistorySeries(nodeID, since, now, bucket, state.Probes)
	if err != nil {
		http.Error(w, "history unavailable", http.StatusInternalServerError)
		return
	}
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": nodeID, "display_name": cfg.DisplayName, "range": rangeName, "bucket_seconds": bucket,
		"series": series, "route": s.publicRouteState(nodeID),
		"history_retention_days": networkHistoryRetentionDays, "route_interval_minutes": int(routeSnapshotInterval / time.Minute),
	})
}

func (s *server) readHistorySeries(nodeID string, since, until time.Time, bucket int64, current []common.ProbeResult) ([]historySeries, error) {
	expected := map[string]common.ProbeResult{}
	order := make([]string, 0, len(current))
	for _, p := range current {
		key := p.Name + "\x00" + p.Target
		expected[key] = p
		order = append(order, key)
	}
	bySeries := map[string]map[int64]*historyAgg{}
	if err := s.scanHistory(nodeID, since, until, func(rec historyDiskRecord) {
		for _, p := range rec.Probes {
			key := p.Name + "\x00" + p.Target
			if len(expected) > 0 {
				if _, ok := expected[key]; !ok {
					continue
				}
			}
			b := (rec.At / bucket) * bucket
			m := bySeries[key]
			if m == nil {
				m = map[int64]*historyAgg{}
				bySeries[key] = m
			}
			a := m[b]
			if a == nil {
				a = &historyAgg{}
				m[b] = a
			}
			success := p.Sent - p.Lost
			if success < 0 {
				success = 0
			}
			if p.LatencyMS >= 0 {
				weight := success
				if weight <= 0 {
					weight = 1
				}
				a.LatencySum += p.LatencyMS * float64(weight)
				a.LatencyN += weight
			}
			a.Sent += p.Sent
			a.Lost += p.Lost
		}
	}); err != nil {
		return nil, err
	}
	if len(order) == 0 {
		for key := range bySeries {
			order = append(order, key)
		}
		sort.Strings(order)
	}
	out := make([]historySeries, 0, len(order))
	for _, key := range order {
		parts := strings.SplitN(key, "\x00", 2)
		name, target := parts[0], ""
		if len(parts) == 2 {
			target = parts[1]
		}
		ser := historySeries{Name: name, Target: target, CurrentLatencyMS: -1}
		if p, ok := expected[key]; ok {
			ser.CurrentLatencyMS = p.LatencyMS
		}
		m := bySeries[key]
		keys := make([]int64, 0, len(m))
		for at := range m {
			keys = append(keys, at)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		for _, at := range keys {
			a := m[at]
			lat := -1.0
			if a.LatencyN > 0 {
				lat = a.LatencySum / float64(a.LatencyN)
			}
			loss := 0.0
			if a.Sent > 0 {
				loss = float64(a.Lost) * 100 / float64(a.Sent)
			}
			ser.Points = append(ser.Points, historyPoint{At: at, LatencyMS: lat, Sent: a.Sent, Lost: a.Lost, LossPct: loss})
			ser.Sent += a.Sent
			ser.Lost += a.Lost
		}
		if ser.Sent > 0 {
			ser.LossPct = float64(ser.Lost) * 100 / float64(ser.Sent)
		}
		out = append(out, ser)
	}
	return out, nil
}

func (s *server) scanHistory(nodeID string, since, until time.Time, fn func(historyDiskRecord)) error {
	if !safeNodeID(nodeID) {
		return errors.New("invalid node id")
	}
	dir := filepath.Join(s.historyDir, nodeID)
	start := since.UTC().Truncate(24 * time.Hour)
	end := until.UTC().Truncate(24 * time.Hour)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		path := filepath.Join(dir, day.Format("2006-01-02")+".ndjson")
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4096), 1<<20)
		for sc.Scan() {
			var rec historyDiskRecord
			if json.Unmarshal(sc.Bytes(), &rec) != nil {
				continue
			}
			at := time.Unix(rec.At, 0)
			if at.Before(since) || at.After(until) {
				continue
			}
			fn(rec)
		}
		err = sc.Err()
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *server) enqueueRouteSnapshot(nodeID string, at time.Time, routes []common.RouteResult) {
	if at.IsZero() || len(routes) == 0 || !safeNodeID(nodeID) {
		return
	}
	at = at.UTC()
	s.routeMu.Lock()
	if prev := s.routeSeen[nodeID]; !prev.IsZero() && !at.After(prev) {
		s.routeMu.Unlock()
		return
	}
	job := routeJob{NodeID: nodeID, At: at, Routes: append([]common.RouteResult(nil), routes...)}
	select {
	case s.routeCh <- job:
		s.routeSeen[nodeID] = at
	default:
		// Keep routeSeen unchanged so a later Agent report can retry the snapshot.
	}
	s.routeMu.Unlock()
}

func (s *server) routeWorker() {
	for job := range s.routeCh {
		if err := s.processRouteJob(job); err != nil {
			fmt.Fprintf(os.Stderr, "MiniProbe route processing failed: %v\n", err)
		}
	}
}

func (s *server) processRouteJob(job routeJob) error {
	type enriched struct {
		result common.RouteResult
		hops   []routeHopState
		path   []string
	}
	var lookupIPs []string
	seenIPs := map[string]bool{}
	for _, rr := range job.Routes {
		for _, hop := range rr.Hops {
			if ip := strings.TrimSpace(hop.IP); ip != "" && !seenIPs[ip] {
				seenIPs[ip] = true
				lookupIPs = append(lookupIPs, ip)
			}
		}
		if ip := strings.TrimSpace(rr.DestinationIP); ip != "" && !seenIPs[ip] {
			seenIPs[ip] = true
			lookupIPs = append(lookupIPs, ip)
		}
	}
	asns := s.lookupASNBatch(lookupIPs)
	items := make([]enriched, 0, len(job.Routes))
	for _, rr := range job.Routes {
		item := enriched{result: rr}
		for _, hop := range rr.Hops {
			asn := asns[hop.IP]
			item.hops = append(item.hops, routeHopState{Hop: hop.Hop, ASN: asn})
			item.path = appendASNPath(item.path, asn)
		}
		item.path = appendASNPath(item.path, asns[rr.DestinationIP])
		items = append(items, item)
	}

	s.routeMu.Lock()
	state := s.routeStates[job.NodeID]
	if state.Carriers == nil {
		state = routeNodeState{Version: 1, NodeID: job.NodeID, Carriers: map[string]routeCarrierState{}}
	}
	state.LastChecked = job.At
	seenCarriers := map[string]bool{}
	for _, item := range items {
		seenCarriers[item.result.Name] = true
		before := state.Carriers[item.result.Name]
		after := updateRouteCarrier(before, item.result, item.hops, item.path, job.At)
		if after.Status == "changed" && !after.ChangedAt.IsZero() && !after.ChangedAt.Equal(before.ChangedAt) {
			state.Events = appendRouteEvent(state.Events, routeChangeEvent{At: after.ChangedAt, Name: after.Name, Type: "changed", From: append([]string(nil), after.Baseline...), To: append([]string(nil), after.Current...)})
		} else if before.Status == "changed" && after.Status == "normal" {
			state.Events = appendRouteEvent(state.Events, routeChangeEvent{At: job.At, Name: after.Name, Type: "recovered", From: append([]string(nil), before.Current...), To: append([]string(nil), after.Baseline...)})
		}
		state.Carriers[item.result.Name] = after
	}
	for name := range state.Carriers {
		if !seenCarriers[name] {
			delete(state.Carriers, name)
		}
	}
	s.routeStates[job.NodeID] = state
	s.routeMu.Unlock()
	return s.persistRouteState(job.NodeID)
}

func updateRouteCarrier(cur routeCarrierState, rr common.RouteResult, hops []routeHopState, path []string, at time.Time) routeCarrierState {
	previousTarget := cur.Target
	previousCurrent := append([]string(nil), cur.Current...)
	previousCandidateCount := cur.CandidateCount
	wasChanged := cur.Status == "changed"
	cur.Name = rr.Name
	cur.Target = rr.Target
	cur.LastChecked = at
	cur.Hops = hops
	cur.Available = rr.Available && len(path) > 0
	if !cur.Available {
		if len(cur.Baseline) == 0 {
			cur.Status = "unavailable"
		}
		return cur
	}
	if len(cur.Baseline) == 0 || (previousTarget != "" && previousTarget != rr.Target) {
		cur.Baseline = append([]string(nil), path...)
		cur.Current = append([]string(nil), path...)
		cur.Status = "normal"
		cur.FirstDifferent = 0
		cur.ChangedAt = time.Time{}
		cur.Candidate = nil
		cur.CandidateCount = 0
		return cur
	}
	cur.Current = append([]string(nil), path...)
	if stringSlicesEqual(cur.Baseline, path) {
		cur.Status = "normal"
		cur.FirstDifferent = 0
		cur.Candidate = nil
		cur.CandidateCount = 0
		return cur
	}
	cur.FirstDifferent = firstDifferentASN(cur.Baseline, path)
	if stringSlicesEqual(cur.Candidate, path) {
		cur.CandidateCount++
	} else {
		cur.Candidate = append([]string(nil), path...)
		cur.CandidateCount = 1
	}
	if cur.CandidateCount >= routeChangeConfirmations {
		if !wasChanged || previousCandidateCount < routeChangeConfirmations || !stringSlicesEqual(previousCurrent, path) {
			cur.ChangedAt = at
		}
		cur.Status = "changed"
	} else if wasChanged {
		cur.Status = "changed"
	} else {
		cur.Status = "checking"
	}
	return cur
}

func appendRouteEvent(events []routeChangeEvent, event routeChangeEvent) []routeChangeEvent {
	events = append(events, event)
	const maxEvents = 50
	if len(events) > maxEvents {
		events = append([]routeChangeEvent(nil), events[len(events)-maxEvents:]...)
	}
	return events
}

func appendASNPath(path []string, asn string) []string {
	asn = strings.TrimSpace(asn)
	if asn == "" {
		return path
	}
	if len(path) == 0 || path[len(path)-1] != asn {
		return append(path, asn)
	}
	return path
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func firstDifferentASN(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i + 1
		}
	}
	if len(a) != len(b) {
		return n + 1
	}
	return 0
}

func (s *server) lookupASNBatch(ips []string) map[string]string {
	out := make(map[string]string, len(ips))
	if len(ips) == 0 {
		return out
	}
	type result struct {
		ip  string
		asn string
	}
	jobs := make(chan string)
	results := make(chan result, len(ips))
	workers := 8
	if len(ips) < workers {
		workers = len(ips)
	}
	for i := 0; i < workers; i++ {
		go func() {
			for ip := range jobs {
				results <- result{ip: ip, asn: s.lookupASN(ip)}
			}
		}()
	}
	go func() {
		for _, ip := range ips {
			jobs <- ip
		}
		close(jobs)
	}()
	for range ips {
		r := <-results
		out[r.ip] = r.asn
	}
	return out
}

func (s *server) lookupASN(rawIP string) string {
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if ip == nil || !publicRouteIP(ip) {
		return ""
	}
	key := ip.String()
	now := time.Now()
	s.asnMu.Lock()
	if c, ok := s.asnCache[key]; ok && now.Before(c.Expires) {
		s.asnMu.Unlock()
		return c.ASN
	}
	s.asnMu.Unlock()
	query := asnQueryName(ip)
	if query == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	txts, err := net.DefaultResolver.LookupTXT(ctx, query)
	asn := ""
	if err == nil {
		for _, txt := range txts {
			fields := strings.Split(txt, "|")
			if len(fields) == 0 {
				continue
			}
			var parts []string
			for _, raw := range strings.Fields(strings.TrimSpace(fields[0])) {
				if _, err := strconv.ParseUint(raw, 10, 32); err == nil {
					parts = append(parts, "AS"+raw)
				}
			}
			if len(parts) > 0 {
				asn = strings.Join(parts, "/")
				break
			}
		}
	}
	ttl := 24 * time.Hour
	if asn == "" {
		ttl = time.Hour
	}
	s.asnMu.Lock()
	s.asnCache[key] = asnCacheEntry{ASN: asn, Expires: now.Add(ttl)}
	s.asnMu.Unlock()
	return asn
}

func publicRouteIP(ip net.IP) bool {
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// RFC 6598 shared address space is not a public Internet hop and should
		// not be submitted to the ASN mapping service.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
	}
	return true
}

func asnQueryName(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", v4[3], v4[2], v4[1], v4[0])
	}
	v6 := ip.To16()
	if v6 == nil {
		return ""
	}
	h := hex.EncodeToString(v6)
	parts := make([]string, 0, len(h))
	for i := len(h) - 1; i >= 0; i-- {
		parts = append(parts, string(h[i]))
	}
	return strings.Join(parts, ".") + ".origin6.asn.cymru.com"
}

func (s *server) persistRouteState(nodeID string) error {
	if !safeNodeID(nodeID) {
		return errors.New("invalid node id")
	}
	s.routeFileMu.Lock()
	defer s.routeFileMu.Unlock()
	s.routeMu.RLock()
	state, ok := s.routeStates[nodeID]
	if !ok {
		s.routeMu.RUnlock()
		return errors.New("route state not found")
	}
	b, err := json.MarshalIndent(state, "", "  ")
	s.routeMu.RUnlock()
	if err != nil {
		return err
	}
	dir := filepath.Join(s.historyDir, nodeID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	path := filepath.Join(dir, "route.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *server) loadRouteStates() {
	entries, err := os.ReadDir(s.historyDir)
	if err != nil {
		return
	}
	for _, d := range entries {
		if !d.IsDir() || !safeNodeID(d.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.historyDir, d.Name(), "route.json"))
		if err != nil {
			continue
		}
		var state routeNodeState
		if json.Unmarshal(b, &state) != nil || state.NodeID != d.Name() {
			continue
		}
		if state.Carriers == nil {
			state.Carriers = map[string]routeCarrierState{}
		}
		s.routeStates[state.NodeID] = state
	}
}

func (s *server) publicRouteState(nodeID string) publicRouteNode {
	s.routeMu.RLock()
	state, ok := s.routeStates[nodeID]
	s.routeMu.RUnlock()
	out := publicRouteNode{Status: "collecting", ASNSource: "Team Cymru IP to ASN Mapping"}
	if !ok {
		return out
	}
	out.LastChecked = state.LastChecked
	for _, e := range state.Events {
		out.Events = append(out.Events, publicRouteEvent{At: e.At, Name: e.Name, Type: e.Type, From: append([]string(nil), e.From...), To: append([]string(nil), e.To...)})
	}
	names := make([]string, 0, len(state.Carriers))
	for name := range state.Carriers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return carrierSortKey(names[i]) < carrierSortKey(names[j]) })
	anyNormal, anyChecking, anyUnavailable := false, false, false
	for _, name := range names {
		c := state.Carriers[name]
		pc := publicRouteCarrier{Name: c.Name, Target: c.Target, Available: c.Available, Baseline: append([]string(nil), c.Baseline...), Current: append([]string(nil), c.Current...), Status: c.Status, FirstDifferent: c.FirstDifferent, LastChecked: c.LastChecked}
		if !c.ChangedAt.IsZero() {
			changedAt := c.ChangedAt
			pc.ChangedAt = &changedAt
		}
		for _, h := range c.Hops {
			pc.Hops = append(pc.Hops, publicRouteHop{Hop: h.Hop, ASN: h.ASN})
		}
		out.Carriers = append(out.Carriers, pc)
		if !c.Available {
			anyUnavailable = true
			continue
		}
		switch c.Status {
		case "changed":
			out.ChangedCount++
		case "checking":
			anyChecking = true
		case "normal":
			anyNormal = true
		case "unavailable":
			anyUnavailable = true
		}
	}
	switch {
	case out.ChangedCount > 0:
		out.Status = "changed"
	case anyChecking:
		out.Status = "checking"
	case anyUnavailable:
		out.Status = "unavailable"
	case anyNormal:
		out.Status = "normal"
	}
	return out
}

func carrierSortKey(name string) string {
	switch {
	case strings.HasSuffix(name, "电信"):
		return "1-" + name
	case strings.HasSuffix(name, "联通"):
		return "2-" + name
	case strings.HasSuffix(name, "移动"):
		return "3-" + name
	default:
		return "9-" + name
	}
}

func (s *server) routeSummary(nodeID string) (string, int, time.Time) {
	s.routeMu.RLock()
	state, ok := s.routeStates[nodeID]
	s.routeMu.RUnlock()
	if !ok {
		return "collecting", 0, time.Time{}
	}
	changed, checking, normal, unavailable := 0, false, false, false
	for _, c := range state.Carriers {
		if !c.Available {
			unavailable = true
			continue
		}
		switch c.Status {
		case "changed":
			changed++
		case "checking":
			checking = true
		case "normal":
			normal = true
		case "unavailable":
			unavailable = true
		}
	}
	switch {
	case changed > 0:
		return "changed", changed, state.LastChecked
	case checking:
		return "checking", 0, state.LastChecked
	case unavailable:
		return "unavailable", 0, state.LastChecked
	case normal:
		return "normal", 0, state.LastChecked
	default:
		return "collecting", 0, state.LastChecked
	}
}

func (s *server) resetRouteBaseline(nodeID string) error {
	if !safeNodeID(nodeID) {
		return errors.New("invalid node id")
	}
	s.routeMu.Lock()
	state, ok := s.routeStates[nodeID]
	if !ok {
		s.routeMu.Unlock()
		return errors.New("该节点尚未采集到路由数据")
	}
	changed := false
	for name, c := range state.Carriers {
		if len(c.Current) == 0 {
			continue
		}
		c.Baseline = append([]string(nil), c.Current...)
		c.Status = "normal"
		c.FirstDifferent = 0
		c.ChangedAt = time.Time{}
		c.Candidate = nil
		c.CandidateCount = 0
		state.Carriers[name] = c
		changed = true
	}
	s.routeStates[nodeID] = state
	s.routeMu.Unlock()
	if !changed {
		return errors.New("该节点当前没有可作为基准的 ASN 路径")
	}
	return s.persistRouteState(nodeID)
}

func (s *server) removeNetworkHistory(nodeID string) {
	if !safeNodeID(nodeID) {
		return
	}
	s.historyMu.Lock()
	delete(s.historyAcc, nodeID)
	delete(s.historyLastProbe, nodeID)
	s.historyMu.Unlock()
	s.routeMu.Lock()
	delete(s.routeStates, nodeID)
	delete(s.routeSeen, nodeID)
	s.routeMu.Unlock()
	_ = os.RemoveAll(filepath.Join(s.historyDir, nodeID))
}
