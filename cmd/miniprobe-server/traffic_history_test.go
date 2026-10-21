package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

func newTrafficHistoryTestServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	s := &server{
		dataFile: filepath.Join(dir, "miniprobe.json"),
		downloadsDir: filepath.Join(dir, "downloads"),
		db: database{Nodes: map[string]nodeConfig{
			"node-1": {ID: "node-1"}, "node-2": {ID: "node-2"},
			"node-a": {ID: "node-a"}, "node-b": {ID: "node-b"},
		}},
	}
	s.initTrafficHistory()
	return s
}

func trafficTestSnapshot(cycle time.Time, in, out, used uint64) common.TrafficSnapshot {
	return common.TrafficSnapshot{
		CycleStart: cycle, InboundBytes: in, OutboundBytes: out,
		UsedBytes: used, LimitBytes: 200_000_000_000, Direction: "outbound",
		ResetDay: 1, ResetTZMinutes: 480,
	}
}

func TestTrafficHistoryWritesOnlyCompletedBucket(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	cycle := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	a := time.Date(2026, 10, 20, 10, 7, 0, 0, time.UTC)
	s.ingestTrafficHistory("node-1", a, trafficTestSnapshot(cycle, 11, 21, 21))
	s.ingestTrafficHistory("node-1", a.Add(4*time.Minute), trafficTestSnapshot(cycle, 19, 39, 39))
	if _, err := os.Stat(filepath.Join(s.trafficHistoryDir, "node-1")); !os.IsNotExist(err) {
		t.Fatalf("report must not write any history file; stat err=%v", err)
	}
	if err := s.flushTrafficHistory(a.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.trafficHistoryDir, "node-1")); !os.IsNotExist(err) {
		t.Fatal("still-current bucket must not be flushed")
	}
	if err := s.flushTrafficHistory(a.Add(9*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := s.readTrafficHistory("node-1", a.Add(-time.Minute), a.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].InboundBytes != 19 || got[0].OutboundBytes != 39 ||
		got[0].UsedBytes != 39 || got[0].CycleStart != cycle.Unix() ||
		got[0].Bucket != a.Truncate(trafficHistoryInterval).Unix() {
		t.Fatalf("bad checkpoint: %+v", got)
	}
	if err := s.flushTrafficHistory(a.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, err := s.readTrafficHistory("node-1", a.Add(-time.Minute), a.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(got, second) {
		t.Fatalf("checkpoint written again: %+v; %v", second, err)
	}
	fi, err := os.Stat(filepath.Join(s.trafficHistoryDir, "node-1", "2026-10-20.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("checkpoint permissions = %v, want 0600", fi.Mode().Perm())
	}
}

func TestTrafficHistoryKeepsBothCyclesInsideOneBucket(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 14, 0, 0, time.UTC)
	prior := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	next := at.Add(30*time.Second)
	s.ingestTrafficHistory("node-2", at, trafficTestSnapshot(prior, 900, 1000, 1000))
	s.ingestTrafficHistory("node-2", next, trafficTestSnapshot(next, 5, 8, 8))
	if err := s.flushTrafficHistory(at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := s.readTrafficHistory("node-2", at.Add(-time.Minute), next.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UsedBytes != 1000 || got[1].UsedBytes != 8 ||
		got[0].CycleStart == got[1].CycleStart {
		t.Fatalf("billing cycle boundary was lost: %+v", got)
	}
}

func TestTrafficHistoryRejectsInvalidAgentSnapshots(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 10, 0, 0, time.UTC)
	good := trafficTestSnapshot(at.Add(-time.Hour), 1, 2, 2)
	s.ingestTrafficHistory("../escape", at, good)
	s.ingestTrafficHistory("node-1", at, common.TrafficSnapshot{})
	future := good
	future.CycleStart = at.Add(time.Hour)
	s.ingestTrafficHistory("node-1", at, future)
	invalidDirection := good
	invalidDirection.Direction = "unknown"
	s.ingestTrafficHistory("node-1", at, invalidDirection)
	if len(s.trafficHistoryPending) != 0 {
		t.Fatalf("invalid snapshots entered history buffer: %v", s.trafficHistoryPending)
	}
}

func TestTrafficHistoryDiskCapKeepsPendingForRetry(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 10, 0, 0, time.UTC)
	s.ingestTrafficHistory("node-1", at, trafficTestSnapshot(at.Add(-time.Hour), 1, 2, 2))
	s.trafficHistoryBytes = trafficHistoryHardLimit - 1
	if err := s.flushTrafficHistory(at.Add(20 * time.Minute)); err == nil {
		t.Fatal("module disk cap must block append")
	}
	if len(s.trafficHistoryPending["node-1"]) != 1 {
		t.Fatal("failed disk append must retain pending checkpoint")
	}
	s.trafficHistoryBytes = 0
	if err := s.flushTrafficHistory(at.Add(20 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(s.trafficHistoryPending) != 0 {
		t.Fatal("successful retry must remove pending checkpoint")
	}
}

func TestTrafficHistoryPrunesExpiredDaysAndAccountsBytes(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	nodeDir := filepath.Join(s.trafficHistoryDir, "node-1")
	if err := os.MkdirAll(nodeDir, 0750); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	oldName := filepath.Join(nodeDir, now.AddDate(0, 0, -100).Format("2006-01-02")+".ndjson")
	newName := filepath.Join(nodeDir, now.AddDate(0, 0, -2).Format("2006-01-02")+".ndjson")
	for _, path := range []string{oldName, newName} {
		if err := os.WriteFile(path, []byte("test\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.pruneTrafficHistory(now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldName); !os.IsNotExist(err) {
		t.Fatal("expired file still exists")
	}
	if _, err := os.Stat(newName); err != nil || s.trafficHistoryBytes != int64(len("test\n")) {
		t.Fatalf("retained file or accounting is wrong: %v; %d", err, s.trafficHistoryBytes)
	}
}

func TestTrafficHistoryReadDeduplicatesAndSkipsCorruptLine(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 10, 0, 0, time.UTC)
	dayDir := filepath.Join(s.trafficHistoryDir, "node-1")
	if err := os.MkdirAll(dayDir, 0750); err != nil {
		t.Fatal(err)
	}
	cycle := at.Add(-time.Hour).Unix()
	makeLine := func(second int64, used uint64) string {
		return fmt.Sprintf(`{"bucket":%d,"at":%d,"cycle_start":%d,"inbound":1,"outbound":%d,"used":%d,"limit":200,"direction":"outbound"}`,
			at.Truncate(trafficHistoryInterval).Unix(), second, cycle, used, used)
	}
	data := strings.Join([]string{makeLine(at.Unix(), 15), "{broken", makeLine(at.Add(time.Minute).Unix(), 20)}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dayDir, "2026-10-20.ndjson"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := s.readTrafficHistory("node-1", at.Add(-time.Minute), at.Add(time.Hour))
	if err != nil || len(got) != 1 || got[0].UsedBytes != 20 {
		t.Fatalf("malformed or duplicated checkpoint: %+v, err=%v", got, err)
	}
}

func TestTrafficHistoryBufferBoundedDuringDiskFailures(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		s.ingestTrafficHistory("node-1", at.Add(time.Duration(i)*trafficHistoryInterval),
			trafficTestSnapshot(at.Add(-time.Hour), uint64(i), uint64(i), uint64(i)))
	}
	if len(s.trafficHistoryPending["node-1"]) != trafficHistoryPendingLimit {
		t.Fatalf("pending checkpoint count = %d", len(s.trafficHistoryPending["node-1"]))
	}
}

func TestTrafficHistoryDeleteNodeRemovesOnlyItsData(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"node-a", "node-b"} {
		s.ingestTrafficHistory(id, at, trafficTestSnapshot(at.Add(-time.Hour), 3, 8, 8))
	}
	if err := s.flushTrafficHistory(at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.removeTrafficHistory("node-a")
	if _, err := os.Stat(filepath.Join(s.trafficHistoryDir, "node-a")); !os.IsNotExist(err) {
		t.Fatal("removed node's traffic data still exists")
	}
	records, err := s.readTrafficHistory("node-b", at.Add(-time.Minute), at.Add(time.Hour))
	if err != nil || len(records) != 1 {
		t.Fatalf("other nodes' history must remain: %+v; %v", records, err)
	}
}

func TestTrafficHistoryNeverBlocksAgentReportDuringBackgroundDiskWork(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 10, 0, 0, time.UTC)
	sample := trafficTestSnapshot(at.Add(-time.Hour), 5, 10, 10)

	// Model a slow background disk operation that holds the history lock.
	// This must not delay authenticated Agent reports or alert processing.
	s.trafficHistoryMu.Lock()
	defer s.trafficHistoryMu.Unlock()
	done := make(chan struct{})
	go func() {
		s.ingestTrafficHistory("node-1", at, sample)
		close(done)
	}()
	select {
	case <-done:
		// An optional history point may be discarded under contention.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("history ingestion blocked an Agent report behind disk I/O")
	}
	if len(s.trafficHistoryPending) != 0 {
		t.Fatal("contended history write must not mutate the pending buffer")
	}
}

func TestTrafficHistoryDeletedNodeCannotBeRecreatedByLateReport(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 5, 0, 0, time.UTC)
	cycle := at.Add(-24 * time.Hour)
	s.ingestTrafficHistory("node-1", at, trafficTestSnapshot(cycle, 100, 200, 200))
	if err := s.flushTrafficHistory(at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	delete(s.db.Nodes, "node-1")
	s.mu.Unlock()
	s.removeTrafficHistory("node-1")
	s.ingestTrafficHistory("node-1", at.Add(2*time.Hour), trafficTestSnapshot(cycle, 200, 400, 400))
	if err := s.flushTrafficHistory(at.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.trafficHistoryDir, "node-1")); !os.IsNotExist(err) {
		t.Fatalf("removed node history was re-created: %v", err)
	}
	if len(s.trafficHistoryPending["node-1"]) != 0 {
		t.Fatal("removed node must never have pending traffic checkpoints")
	}
}

func TestTrafficHistoryNeverWaitsForMainStateLock(t *testing.T) {
	s := newTrafficHistoryTestServer(t)
	at := time.Date(2026, 10, 20, 10, 5, 0, 0, time.UTC)
	sample := trafficTestSnapshot(at.Add(-time.Hour), 1, 2, 2)
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.ingestTrafficHistory("node-1", at, sample)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("traffic history blocked the Agent report on main state lock")
	}
}
