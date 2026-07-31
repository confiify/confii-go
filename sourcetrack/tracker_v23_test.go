// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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
		t.Fatalf(":tracker poisoned via si.Value mutation. tracker port = %v, want 8080", mp2["port"])
	}
}

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
		t.Fatalf(":tracker poisoned via si.Value byte mutation. tracker = %q, want %q", string(b2), "hello")
	}
}

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
		t.Fatalf(":tracker history poisoned. history[0].v = %v, want 1", mp2["v"])
	}
}

func TestExportDebugReport_NoRaceWithTrackValue(t *testing.T) {
	tr := NewTracker(false)
	for i := range 8 {
		tr.TrackValue("key"+string(rune('0'+i)),
			map[string]any{"i": i},
			"runtime", "runtime", "test",
		)
	}

	tmp := t.TempDir()
	out := filepath.Join(tmp, "report.json")

	stop := make(chan struct{})

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
				tr.TrackValue("key"+string(rune('0'+(i%8))),
					map[string]any{"i": i},
					"runtime", "runtime", "test",
				)
				i++
			}
		}
	}()

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

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf(":ExportDebugReport produced malformed JSON:%v", err)
	}
}

func TestSnapshot_ValueDeepCopy_RestoreIsTrueRollback(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("svc", map[string]any{"port": 8080}, "a.yaml", "yaml", "test")

	snap := tr.Snapshot()

	si := tr.GetSourceInfo("svc")
	if mp, ok := si.Value.(map[string]any); ok {
		mp["port"] = 9999
	}

	tr.TrackValue("svc", map[string]any{"port": 7777}, "b.yaml", "yaml", "test")

	tr.Restore(snap)

	si2 := tr.GetSourceInfo("svc")
	mp2, ok := si2.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", si2.Value)
	}
	if mp2["port"] != 8080 {
		t.Fatalf("Restore did not produce a true rollback:port = %v, want 8080 (snapshot().Value aliasing)", mp2["port"])
	}
}
