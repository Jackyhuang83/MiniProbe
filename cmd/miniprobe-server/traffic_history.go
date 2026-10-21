package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jackyhuang83/MiniProbe/internal/common"
)

// Traffic history is advisory only. Agent billing and protection always use the
// live Agent snapshot; these Server-side checkpoints never feed either policy.
const (
	trafficHistoryInterval      = 15 * time.Minute
	trafficHistoryRetentionDays = 90
	trafficHistoryHardLimit     = int64(64 << 20)
	trafficHistoryPendingLimit  = 16 // bounded even when storage is unavailable
)

type trafficHistoryCheckpoint struct {
	Bucket         int64  `json:"bucket"`       // UTC bucket start, Unix seconds
	At             int64  `json:"at"`           // Server receive time, Unix seconds
	CycleStart     int64  `json:"cycle_start"`  // Agent billing-cycle start
	InboundBytes   uint64 `json:"inbound"`
	OutboundBytes  uint64 `json:"outbound"`
	UsedBytes      uint64 `json:"used"`
	LimitBytes     uint64 `json:"limit"`
	Direction      string `json:"direction"`
	ResetDay       int    `json:"reset_day"`
	ResetTZMinutes int    `json:"reset_tz_minutes"`
}

type trafficHistoryKey struct {
	Bucket     int64
	CycleStart int64
}

// initTrafficHistory creates an independent storage area and enforces its cap.
// Errors are nonfatal: monitoring and Agent replies must continue normally.
func (s *server) initTrafficHistory() {
	if s.trafficHistoryDir == "" {
		s.trafficHistoryDir = filepath.Join(filepath.Dir(s.dataFile), "traffic-history")
	}
	if s.trafficHistoryPending == nil {
		s.trafficHistoryPending = make(map[string]map[trafficHistoryKey]trafficHistoryCheckpoint)
	}
	if err := os.MkdirAll(s.trafficHistoryDir, 0750); err != nil {
		fmt.Fprintf(os.Stderr, "MiniProbe traffic history initialization failed: %v\n", err)
		return
	}
	if err := s.pruneTrafficHistory(time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "MiniProbe traffic history pruning failed: %v\n", err)
	}
}

// ingestTrafficHistory only updates a small, bounded memory buffer. It never
// reads or writes the filesystem from the Agent's HTTP report path.
func (s *server) ingestTrafficHistory(nodeID string, at time.Time, traffic common.TrafficSnapshot) {
	if !safeNodeID(nodeID) || traffic.CycleStart.IsZero() || at.IsZero() ||
		traffic.CycleStart.After(at.Add(5*time.Minute)) {
		return
	}
	// Lock order is always main state -> traffic history, matching the
	// delete-node path. Keep the read lock until after the best-effort
	// checkpoint is enqueued, so a concurrent deletion cannot re-create
	// the removed node's history. Never block an Agent report on either lock.
	if !s.mu.TryRLock() {
		return
	}
	defer s.mu.RUnlock()
	if _, registered := s.db.Nodes[nodeID]; !registered {
		return
	}
	switch traffic.Direction {
	case "inbound", "outbound", "total":
	default:
		return
	}
	at = at.UTC()
	rec := trafficHistoryCheckpoint{
		Bucket: at.Truncate(trafficHistoryInterval).Unix(), At: at.Unix(),
		CycleStart: traffic.CycleStart.Unix(), InboundBytes: traffic.InboundBytes,
		OutboundBytes: traffic.OutboundBytes, UsedBytes: traffic.UsedBytes,
		LimitBytes: traffic.LimitBytes, Direction: traffic.Direction,
		ResetDay: traffic.ResetDay, ResetTZMinutes: traffic.ResetTZMinutes,
	}
	key := trafficHistoryKey{Bucket: rec.Bucket, CycleStart: rec.CycleStart}
	// History is best-effort. A slow disk, cleanup or Dev2 history read may
	// hold this lock in a background worker; never wait for it in /report.
	// The next Agent report can supply a fresher cumulative checkpoint.
	if !s.trafficHistoryMu.TryLock() {
		return
	}
	defer s.trafficHistoryMu.Unlock()
	if s.trafficHistoryPending == nil {
		s.trafficHistoryPending = make(map[string]map[trafficHistoryKey]trafficHistoryCheckpoint)
	}
	pending := s.trafficHistoryPending[nodeID]
	if pending == nil {
		pending = make(map[trafficHistoryKey]trafficHistoryCheckpoint)
		s.trafficHistoryPending[nodeID] = pending
	}
	// Keep the latest cumulative counter in each interval, with distinct
	// entries for two billing cycles that cross within one interval.
	if old, ok := pending[key]; !ok || rec.At >= old.At {
		pending[key] = rec
	}
	// Prefer missing history to unbounded RAM use during a disk failure.
	for len(pending) > trafficHistoryPendingLimit {
		var oldest trafficHistoryKey
		first := true
		for k := range pending {
			if first || k.Bucket < oldest.Bucket || (k.Bucket == oldest.Bucket && k.CycleStart < oldest.CycleStart) {
				oldest, first = k, false
			}
		}
		delete(pending, oldest)
	}
}

func (s *server) trafficHistoryLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastPrune := time.Now().UTC()
	var lastFailureLog time.Time
	for now := range ticker.C {
		now = now.UTC()
		if err := s.flushTrafficHistory(now); err != nil && now.Sub(lastFailureLog) >= 15*time.Minute {
			fmt.Fprintf(os.Stderr, "MiniProbe traffic history write failed: %v\n", err)
			lastFailureLog = now
		}
		if now.Sub(lastPrune) >= 6*time.Hour {
			if err := s.pruneTrafficHistory(now); err != nil && now.Sub(lastFailureLog) >= 15*time.Minute {
				fmt.Fprintf(os.Stderr, "MiniProbe traffic history prune failed: %v\n", err)
				lastFailureLog = now
			}
			lastPrune = now
		}
	}
}

// flushTrafficHistory is called by the background loop, not by report handlers.
// Completed buckets only: each node/cycle/bucket is written at most once.
func (s *server) flushTrafficHistory(now time.Time) error {
	current := now.UTC().Truncate(trafficHistoryInterval).Unix()
	s.trafficHistoryMu.Lock()
	defer s.trafficHistoryMu.Unlock()
	type readyEntry struct {
		nodeID string
		key    trafficHistoryKey
		rec    trafficHistoryCheckpoint
	}
	var ready []readyEntry
	for id, pending := range s.trafficHistoryPending {
		for key, rec := range pending {
			if key.Bucket < current {
				ready = append(ready, readyEntry{nodeID: id, key: key, rec: rec})
			}
		}
	}
	if len(ready) == 0 {
		return nil
	}
	sort.Slice(ready, func(i, j int) bool {
		if ready[i].rec.At != ready[j].rec.At {
			return ready[i].rec.At < ready[j].rec.At
		}
		return ready[i].nodeID < ready[j].nodeID
	})
	// Network history, binaries and the main JSON database share the same 2 GiB
	// installation budget. Check once per batch; decrement for each append.
	remaining := int64(storageHardLimit) - int64(s.storageUsage())
	var firstErr error
	for _, entry := range ready {
		line, err := json.Marshal(entry.rec)
		if err == nil {
			size := int64(len(line) + 1)
			if size > remaining || s.trafficHistoryBytes+size > trafficHistoryHardLimit {
				err = errors.New("traffic history storage budget reached")
			} else {
				err = s.appendTrafficHistoryLocked(entry.nodeID, entry.rec, line)
				if err == nil {
					remaining -= size
					s.trafficHistoryBytes += size
				}
			}
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delete(s.trafficHistoryPending[entry.nodeID], entry.key)
		if len(s.trafficHistoryPending[entry.nodeID]) == 0 {
			delete(s.trafficHistoryPending, entry.nodeID)
		}
	}
	return firstErr
}

func (s *server) appendTrafficHistoryLocked(nodeID string, rec trafficHistoryCheckpoint, line []byte) error {
	if !safeNodeID(nodeID) {
		return errors.New("invalid traffic history node id")
	}
	dir := filepath.Join(s.trafficHistoryDir, nodeID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	// Never follow a locally planted directory symlink outside the data dir.
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("traffic history node path is not a directory")
	}
	day := time.Unix(rec.Bucket, 0).UTC().Format("2006-01-02")
	path := filepath.Join(dir, day+".ndjson")
	if st, err := os.Lstat(path); err == nil && !st.Mode().IsRegular() {
		return errors.New("traffic history path is not a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(line, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// pruneTrafficHistory ages out records independently of network-history and
// retains at most 64 MiB. Oldest dated files are removed first.
func (s *server) pruneTrafficHistory(now time.Time) error {
	s.trafficHistoryMu.Lock()
	defer s.trafficHistoryMu.Unlock()
	return s.pruneTrafficHistoryLocked(now)
}

func (s *server) pruneTrafficHistoryLocked(now time.Time) error {
	type diskFile struct {
		path string
		day  string
		size int64
	}
	cutoff := now.UTC().AddDate(0, 0, -trafficHistoryRetentionDays).Format("2006-01-02")
	var files []diskFile
	var total int64
	err := filepath.WalkDir(s.trafficHistoryDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".ndjson") {
			return nil
		}
		day := strings.TrimSuffix(d.Name(), ".ndjson")
		if _, err := time.Parse("2006-01-02", day); err != nil {
			return nil
		}
		if day < cutoff {
			return os.Remove(path)
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, diskFile{path: path, day: day, size: st.Size()})
		total += st.Size()
		return nil
	})
	s.trafficHistoryBytes = total
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].day == files[j].day {
			return files[i].path < files[j].path
		}
		return files[i].day < files[j].day
	})
	for _, file := range files {
		if total <= trafficHistoryHardLimit {
			break
		}
		if err := os.Remove(file.path); err != nil {
			return err
		}
		total -= file.size
	}
	s.trafficHistoryBytes = total
	return nil
}

// readTrafficHistory gives Dev2 an internal read path without exposing any new
// unauthenticated endpoint. Partial/corrupt JSON lines are ignored.
func (s *server) readTrafficHistory(nodeID string, from, until time.Time) ([]trafficHistoryCheckpoint, error) {
	if !safeNodeID(nodeID) || until.Before(from) {
		return nil, errors.New("invalid traffic history query")
	}
	s.trafficHistoryMu.Lock()
	defer s.trafficHistoryMu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.trafficHistoryDir, nodeID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dedup := make(map[trafficHistoryKey]trafficHistoryCheckpoint)
	for _, file := range entries {
		if !file.Type().IsRegular() || !strings.HasSuffix(file.Name(), ".ndjson") {
			continue
		}
		day := strings.TrimSuffix(file.Name(), ".ndjson")
		dayTime, e := time.Parse("2006-01-02", day)
		if e != nil || dayTime.After(until) || dayTime.Add(24*time.Hour).Before(from) {
			continue
		}
		f, e := os.Open(filepath.Join(s.trafficHistoryDir, nodeID, file.Name()))
		if e != nil {
			return nil, e
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024), 32*1024)
		for scanner.Scan() {
			var rec trafficHistoryCheckpoint
			if json.Unmarshal(scanner.Bytes(), &rec) != nil || rec.CycleStart <= 0 ||
				rec.Bucket != time.Unix(rec.At, 0).UTC().Truncate(trafficHistoryInterval).Unix() {
				continue
			}
			at := time.Unix(rec.At, 0)
			if at.Before(from) || at.After(until) {
				continue
			}
			key := trafficHistoryKey{Bucket: rec.Bucket, CycleStart: rec.CycleStart}
			if previous, ok := dedup[key]; !ok || rec.At >= previous.At {
				dedup[key] = rec
			}
		}
		scanErr := scanner.Err()
		closeErr := f.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	out := make([]trafficHistoryCheckpoint, 0, len(dedup))
	for _, rec := range dedup {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At == out[j].At {
			return out[i].CycleStart < out[j].CycleStart
		}
		return out[i].At < out[j].At
	})
	return out, nil
}

func (s *server) removeTrafficHistory(nodeID string) {
	if !safeNodeID(nodeID) {
		return
	}
	s.trafficHistoryMu.Lock()
	defer s.trafficHistoryMu.Unlock()
	delete(s.trafficHistoryPending, nodeID)
	_ = os.RemoveAll(filepath.Join(s.trafficHistoryDir, nodeID))
	_ = s.pruneTrafficHistoryLocked(time.Now().UTC())
}
