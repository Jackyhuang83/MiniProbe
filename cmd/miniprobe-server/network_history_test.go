package main

import (
	"encoding/json"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

func TestASNQueryNames(t *testing.T) {
	if got := asnQueryName(net.ParseIP("216.90.108.31")); got != "31.108.90.216.origin.asn.cymru.com" {
		t.Fatalf("IPv4 query got %q", got)
	}
	want6 := "8.6.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.2.0.0.b.0.6.8.4.1.0.0.2.origin6.asn.cymru.com"
	if got := asnQueryName(net.ParseIP("2001:4860:b002::68")); got != want6 {
		t.Fatalf("IPv6 query got %q want %q", got, want6)
	}
}

func TestRouteChangeRequiresConfirmation(t *testing.T) {
	at := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	rr := common.RouteResult{Name: "浙江联通", Target: "202.106.0.20", Available: true}
	cur := updateRouteCarrier(routeCarrierState{}, rr, nil, []string{"AS54801", "AS4837"}, at)
	if cur.Status != "normal" || !stringSlicesEqual(cur.Baseline, []string{"AS54801", "AS4837"}) {
		t.Fatalf("initial baseline failed: %+v", cur)
	}
	changed := []string{"AS18099", "AS4837"}
	cur = updateRouteCarrier(cur, rr, nil, changed, at.Add(30*time.Minute))
	if cur.Status != "checking" || cur.CandidateCount != 1 || cur.FirstDifferent != 1 {
		t.Fatalf("first differing route should be checking: %+v", cur)
	}
	cur = updateRouteCarrier(cur, rr, nil, changed, at.Add(60*time.Minute))
	if cur.Status != "changed" || cur.ChangedAt.IsZero() || cur.CandidateCount != 2 {
		t.Fatalf("second matching route should confirm change: %+v", cur)
	}
	if !cur.ChangedAt.Equal(at.Add(60 * time.Minute)) {
		t.Fatalf("change time should be confirmation time, got %v", cur.ChangedAt)
	}
	cur = updateRouteCarrier(cur, rr, nil, []string{"AS54801", "AS4837"}, at.Add(90*time.Minute))
	if cur.Status != "normal" || cur.FirstDifferent != 0 {
		t.Fatalf("baseline recovery should return normal: %+v", cur)
	}
}

func TestHistoryMinuteAggregationAndReadback(t *testing.T) {
	dir := t.TempDir()
	s := &server{
		historyDir: dir, historyAcc: map[string]*historyMinuteAccumulator{}, historyLastProbe: map[string]time.Time{},
		routeStates: map[string]routeNodeState{}, routeSeen: map[string]time.Time{}, asnCache: map[string]asnCacheEntry{},
	}
	nodeID := "node-abc123"
	base := time.Now().UTC().Truncate(time.Minute).Add(5 * time.Second)
	p1 := common.ProbeResult{Name: "广州联通", Target: "210.21.4.130", LatencyMS: 100, SampleSent: 4, SampleLost: 0}
	p2 := common.ProbeResult{Name: "广州联通", Target: "210.21.4.130", LatencyMS: 120, SampleSent: 4, SampleLost: 1}
	s.ingestProbeSample(nodeID, base, "tcp", []common.ProbeResult{p1})
	s.ingestProbeSample(nodeID, base.Add(10*time.Second), "tcp", []common.ProbeResult{p2})
	s.flushCompletedHistoryMinutes(base.Add(time.Minute))
	series, err := s.readHistorySeries(nodeID, base.Add(-time.Minute), base.Add(2*time.Minute), 60, []common.ProbeResult{p2})
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || len(series[0].Points) != 1 {
		t.Fatalf("unexpected series: %+v", series)
	}
	got := series[0]
	if got.Sent != 8 || got.Lost != 1 || math.Abs(got.LossPct-12.5) > 0.001 {
		t.Fatalf("unexpected loss aggregation: %+v", got)
	}
	wantLatency := (100.0*4 + 120.0*3) / 7
	if math.Abs(got.Points[0].LatencyMS-wantLatency) > 0.1 {
		t.Fatalf("latency got %.3f want %.3f", got.Points[0].LatencyMS, wantLatency)
	}
}

func TestPublicRouteStateDoesNotExposeHopIPs(t *testing.T) {
	s := &server{routeStates: map[string]routeNodeState{
		"node-1": {
			Version: 1, NodeID: "node-1", LastChecked: time.Now(),
			Carriers: map[string]routeCarrierState{
				"广州联通": {Name: "广州联通", Target: "210.21.4.130", Available: true, Status: "normal", Baseline: []string{"AS4837"}, Current: []string{"AS4837"}, Hops: []routeHopState{{Hop: 1, ASN: "AS4837"}}},
			},
		},
	}}
	b, err := json.Marshal(s.publicRouteState("node-1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "203.0.113.9") || strings.Contains(string(b), `"ip"`) {
		t.Fatalf("public route payload leaked hop IP: %s", b)
	}
	if !strings.Contains(string(b), "AS4837") {
		t.Fatalf("public route payload lost ASN path: %s", b)
	}
}

func TestHistoryRangeBuckets(t *testing.T) {
	now := time.Now()
	_, _, day := historyRange("day", now)
	_, _, week := historyRange("week", now)
	_, _, month := historyRange("month", now)
	if day != 60 || week != 300 || month != 1800 {
		t.Fatalf("unexpected buckets: %d %d %d", day, week, month)
	}
}

func TestRouteEventHistoryIsBounded(t *testing.T) {
	var events []routeChangeEvent
	for i := 0; i < 75; i++ {
		events = appendRouteEvent(events, routeChangeEvent{At: time.Unix(int64(i), 0), Name: "联通", Type: "changed"})
	}
	if len(events) != 50 {
		t.Fatalf("event history len=%d want 50", len(events))
	}
	if events[0].At.Unix() != 25 || events[len(events)-1].At.Unix() != 74 {
		t.Fatalf("unexpected retained event window: first=%v last=%v", events[0].At, events[len(events)-1].At)
	}
}
