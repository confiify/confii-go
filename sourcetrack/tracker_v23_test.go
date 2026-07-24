// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

// V-04 / V-10 (Wave 23) — Negative tests for the strengthened
// cloneSourceInfo / ExportDebugReport contracts.
//
// V-04: pre-fix cloneSourceInfo bit-copied SourceInfo and only
// duplicated the History slice. SourceInfo.Value remained a live
// pointer into the tracker's backing map — a caller mutating
// si.Value.(map[string]any)["k"] poisoned the tracker through the
// shared reference even though the doc explicitly promised "Mutating
// returned values does not affect tracker state."
//
// V-10: pre-fix ExportDebugReport populated the report map with LIVE
// *SourceInfo pointers and then released the read lock before
// marshaling, racing against concurrent TrackValue mutations of those
// fields. The race trips `go test -race`.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// V-04_a — mutating si.Value.(map[string]any) does not poison the tracker.
func TestGetSourceInfo_ValueMapEntry_NotAliased(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("svc", map[string]any{"port": 8080}, "test.yaml", "yaml", "test")

	si := tr.GetSourceInfo("svc")
	if si == nil {
		t.Fatalf("GetSourceInfo returned nil")
	}
	mp, ok := si.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", si.Value)
	}
	mp["port"] = 9999

	si2 := tr.GetSourceInfo("svc")
	mp2, ok := si2.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", si2.Value)
	}
	if mp2["port"] != 8080 {
		t.Fatalf("V-04: tracker poisoned via si.Value mutation. tracker port = %v, want 8080", mp2["port"])
	}
}

// V-04_b — typed []byte values returned from GetSourceInfo are not aliased.
func TestGetSourceInfo_ByteSliceValue_NotAliased(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("blob", []byte("hello"), "runtime", "runtime", "test")

	si := tr.GetSourceInfo("blob")
	b, ok := si.Value.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", si.Value)
	}
	b[0] = 'X'

	si2 := tr.GetSourceInfo("blob")
	b2, ok := si2.Value.([]byte)
	if !ok {
		t.Fatalf("expected []byte, got %T", si2.Value)
	}
	if string(b2) != "hello" {
		t.Fatalf("V-04: tracker poisoned via si.Value byte mutation. tracker = %q, want %q", string(b2), "hello")
	}
}

// V-04_c — mutating an OverrideEntry.Value (map) in History returned
// by GetOverrideHistory does not poison the tracker.
func TestGetOverrideHistory_EntryValue_NotAliased(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("svc", map[string]any{"v": 1}, "a.yaml", "yaml", "test")
	tr.TrackValue("svc", map[string]any{"v": 2}, "b.yaml", "yaml", "test")

	hist := tr.GetOverrideHistory("svc")
	if len(hist) == 0 {
		t.Fatalf("expected at least one history entry, got 0")
	}
	mp, ok := hist[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", hist[0].Value)
	}
	mp["v"] = 999

	hist2 := tr.GetOverrideHistory("svc")
	mp2, ok := hist2[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", hist2[0].Value)
	}
	if mp2["v"] != 1 {
		t.Fatalf("V-04: tracker history poisoned. history[0].v = %v, want 1", mp2["v"])
	}
}

// V-10 — ExportDebugReport must not race concurrent TrackValue.
//
// Pre-V-10 the report was populated with LIVE *SourceInfo pointers and
// then marshaled outside the RLock — a concurrent writer hitting
// existing.Value / existing.Timestamp tripped the race detector.
//
// This test must be run with `go test -race`. Without -race the test
// still exercises the code path but the assertion about no data race
// can only be checked structurally.
func TestExportDebugReport_NoRaceWithTrackValue(t *testing.T) {
	// debug=false keeps History short; we only need the writer to
	// mutate existing.Value / SourceFile / LoaderType / Timestamp in
	// place to expose the pre-V-10 race.
	tr := NewTracker(false)
	for i := range 8 {
		tr.TrackValue(
			"key"+string(rune('0'+i)),
			map[string]any{"i": i},
			"runtime", "runtime", "test",
		)
	}

	tmp := t.TempDir()
	out := filepath.Join(tmp, "report.json")

	stop := make(chan struct{})

	// Concurrent writer thrashes existing.Value / Timestamp / etc.
	// Tracked by a separate WaitGroup so we can stop+drain it AFTER
	// the exporters finish. Putting the writer in the same WaitGroup
	// as the exporters would deadlock the test (writer never returns
	// until stop closes; stop closes only after wg.Wait()).
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				tr.TrackValue(
					"key"+string(rune('0'+(i%8))),
					map[string]any{"i": i},
					"runtime", "runtime", "test",
				)
				i++
			}
		}
	}()

	// Concurrent exporters read those same fields. Pre-V-10 this races
	// against the writer above and trips `go test -race`.
	var expWG sync.WaitGroup
	for range 4 {
		expWG.Add(1)
		go func() {
			defer expWG.Done()
			for range 20 {
				if err := tr.ExportDebugReport(out); err != nil {
					t.Errorf("ExportDebugReport: %v", err)
					return
				}
			}
		}()
	}

	expWG.Wait()
	close(stop)
	writerWG.Wait()

	// Verify the last-written report parses as JSON — a torn write
	// from the pre-V-10 race could surface as malformed JSON.
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("V-10: ExportDebugReport produced malformed JSON: %v", err)
	}
}

// V-04 + V-10 cross-check — Snapshot returns deep-copied *SourceInfo
// values whose .Value field is isolated from concurrent TrackValue.
// A tracker.Restore against a snapshot must therefore be a true rollback
// even when TrackValue mutated the source between Snapshot and Restore.
func TestSnapshot_ValueDeepCopy_RestoreIsTrueRollback(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("svc", map[string]any{"port": 8080}, "a.yaml", "yaml", "test")

	snap := tr.Snapshot()

	// Mutate the live tracker's .Value via a poisoning attempt on
	// the value we just inserted (in the pre-V-04 world a caller could
	// reach the live map via GetSourceInfo).
	si := tr.GetSourceInfo("svc")
	if mp, ok := si.Value.(map[string]any); ok {
		mp["port"] = 9999
	}
	// And via a legitimate writer.
	tr.TrackValue("svc", map[string]any{"port": 7777}, "b.yaml", "yaml", "test")

	tr.Restore(snap)

	si2 := tr.GetSourceInfo("svc")
	mp2, ok := si2.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", si2.Value)
	}
	if mp2["port"] != 8080 {
		t.Fatalf("Restore did not produce a true rollback: port = %v, want 8080 (V-04 snapshot.Value aliasing)", mp2["port"])
	}
}
