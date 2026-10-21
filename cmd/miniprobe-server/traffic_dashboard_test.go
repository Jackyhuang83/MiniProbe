package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

func dashboardTestDays(start, end time.Time, zone *time.Location) map[string]*dashboardTrafficDay {
	days := make(map[string]*dashboardTrafficDay)
	for d := start.In(zone); d.Before(end.In(zone)); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		days[date] = &dashboardTrafficDay{Date: date}
	}
	return days
}

func TestTrafficDashboardDateFilterAndTimezone(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	from, to, start, end, err := trafficDashboardDates("2026-10-19|2026-10-20", now, 540)
	if err != nil {
		t.Fatal(err)
	}
	if from != "2026-10-19" || to != "2026-10-20" ||
		start != (time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)) ||
		end != (time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("invalid timezone boundaries: %s %s %s %s", from, to, start, end)
	}
	for _, raw := range []string{"2026-10-20|2026-10-19", "2026-01-01|2026-10-20",
		"2026-10-21|2026-10-22", "2026-02-30|2026-03-01", "hi|2026-10-21"} {
		if _, _, _, _, err := trafficDashboardDates(raw, now, 540); err == nil {
			t.Fatalf("invalid date filter accepted: %s", raw)
		}
	}
}

func TestTrafficDashboardDailyDeltasPreserveCycleAndMidnight(t *testing.T) {
	zone := time.FixedZone("JST", 540*60)
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, zone).UTC()
	end := start.Add(48 * time.Hour)
	a := time.Date(2026, 10, 19, 23, 50, 0, 0, zone)
	b := a.Add(20 * time.Minute)
	cycleA := time.Date(2026, 10, 1, 0, 0, 0, 0, zone).Unix()
	cycleB := b.Add(time.Minute).Unix()
	rec := func(at time.Time, cycle int64, in, out, used uint64) trafficHistoryCheckpoint {
		return trafficHistoryCheckpoint{
			At: at.Unix(), Bucket: at.UTC().Truncate(trafficHistoryInterval).Unix(),
			CycleStart: cycle, Direction: "outbound", LimitBytes: 1_000, ResetDay: 1,
			ResetTZMinutes: 540, InboundBytes: in, OutboundBytes: out, UsedBytes: used,
		}
	}
	records := []trafficHistoryCheckpoint{
		rec(a, cycleA, 100, 200, 200),
		rec(b, cycleA, 125, 260, 260),
		rec(b.Add(time.Minute), cycleB, 5, 9, 9),
		rec(b.Add(25*time.Minute), cycleB, 10, 30, 30),
	}
	days := dashboardTestDays(start, end, zone)
	got, points, incomplete := aggregateTrafficNode(records, start, end, zone, days)
	if got.InboundBytes != 30 || got.OutboundBytes != 81 || got.UsedBytes != 81 || points != 4 {
		t.Fatalf("wrong delta; got %+v; points %d", got, points)
	}
	if days["2026-10-19"].UsedBytes != 0 || days["2026-10-20"].UsedBytes != 81 {
		t.Fatalf("cross-midnight billing incorrectly attributed: %+v", days)
	}
	if !incomplete || !days["2026-10-19"].Incomplete || !days["2026-10-20"].Incomplete {
		t.Fatalf("first samples of two billing cycles must be marked as baselines: %+v", days)
	}
}

func TestTrafficDashboardSkipsLongGapsAndRegressedCounters(t *testing.T) {
	zone := time.UTC
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, zone)
	end := start.Add(24 * time.Hour)
	cycle := start.Add(-24 * time.Hour).Unix()
	records := []trafficHistoryCheckpoint{
		{At: start.Add(time.Hour).Unix(), CycleStart: cycle, Direction: "outbound", LimitBytes: 100, InboundBytes: 100, OutboundBytes: 100, UsedBytes: 100},
		{At: start.Add(6*time.Hour).Unix(), CycleStart: cycle, Direction: "outbound", LimitBytes: 100, InboundBytes: 500, OutboundBytes: 500, UsedBytes: 500},
		{At: start.Add(6*time.Hour+time.Minute).Unix(), CycleStart: cycle, Direction: "outbound", LimitBytes: 100, InboundBytes: 400, OutboundBytes: 400, UsedBytes: 400},
		{At: start.Add(7*time.Hour).Unix(), CycleStart: cycle, Direction: "outbound", LimitBytes: 100, InboundBytes: 430, OutboundBytes: 440, UsedBytes: 440},
	}
	days := dashboardTestDays(start, end, zone)
	got, _, incomplete := aggregateTrafficNode(records, start, end, zone, days)
	if got.UsedBytes != 40 || got.InboundBytes != 30 || !incomplete {
		t.Fatalf("long gap or reset got counted: %+v; incomplete=%t", got, incomplete)
	}
}

func TestTrafficDashboardEndpointUsesLiveQuotaAndDoesNotMutatePolicy(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	now := time.Now().UTC()
	at := time.Date(now.Year(), now.Month(), now.Day()-1, 12, 0, 0, 0, time.UTC)
	cycle := at.Add(-24 * time.Hour)
	s.db = database{
		Settings: settings{DashboardMode: dashboardPublic},
		Nodes: map[string]nodeConfig{
			"node-a": {ID: "node-a", DisplayName: "Alpha", MonthlyTrafficLimit: 2000, TrafficDirection: "outbound"},
		},
		States: map[string]common.NodeView{
			"node-a": {
				Report: common.Report{Traffic: common.TrafficSnapshot{
					CycleStart: cycle, UsedBytes: 1800, LimitBytes: 2000, Direction: "outbound",
				}},
				LastSeen: now,
			},
		},
	}
	s.ingestTrafficHistory("node-a", at, trafficTestSnapshot(cycle, 100, 200, 200))
	s.ingestTrafficHistory("node-a", at.Add(30*time.Minute), trafficTestSnapshot(cycle, 150, 260, 260))
	if err := s.flushTrafficHistory(now); err != nil {
		t.Fatal(err)
	}
	date := at.Format("2006-01-02")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/dashboard?from="+date+"&to="+date+"&tz=0", nil)
	w := httptest.NewRecorder()
	s.trafficDashboardAPI(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sensitive dashboard response must not be cached")
	}
	var out dashboardTrafficResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Nodes) != 1 || len(out.Days) != 1 {
		t.Fatalf("unexpected nodes/days: %+v", out)
	}
	node := out.Nodes[0]
	if out.Totals.UsedBytes != 60 || node.History.UsedBytes != 60 ||
		node.QuotaBytes != 2000 || node.CycleUsedBytes != 1800 || node.RemainingBytes != 200 ||
		len(node.Days) != 1 || node.Days[0].UsedBytes != 60 {
		t.Fatalf("billing/history fields incorrect: %+v, %+v", out.Totals, node)
	}
	if s.db.States["node-a"].Traffic.UsedBytes != 1800 || s.db.Nodes["node-a"].MonthlyTrafficLimit != 2000 {
		t.Fatal("read-only dashboard modified billing state")
	}
	for _, raw := range []string{"?tz=9999", "?from=2026-02-30&to=2026-03-01"} {
		w := httptest.NewRecorder()
		s.trafficDashboardAPI(w, httptest.NewRequest(http.MethodGet, "/api/v1/traffic/dashboard"+raw, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid query %s accepted: %d", raw, w.Code)
		}
	}
}

func TestTrafficDashboardRespectsDashboardAuth(t *testing.T) {
	s := &server{db: database{Settings: settings{DashboardMode: dashboardProtected}}}
	handler := s.dashboardOnly(s.trafficDashboardAPI)
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/api/v1/traffic/dashboard", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("protected dashboard API leaked data: HTTP %d", w.Code)
	}
}
