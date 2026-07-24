// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package watch

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// safeBuffer is a goroutine-safe wrapper around bytes.Buffer used as the
// destination for slog handlers in these tests. The watcher loop logs
// from a background goroutine while the test goroutine reads buf.String()
// to assert observables; both ends must be serialized to keep -race
// happy.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// G20: dynamic file watching needs a source-capability model.
//
// These tests pin two distinct, observable behaviors:
//
//  1. Non-file sources (HTTP URLs, "environment:" markers, cloud-store ids)
//     must be silently skipped with a warning log, NOT registered with
//     fsnotify (which would either error out or attempt to watch a
//     non-existent path).
//  2. Watchers must respond to atomic-save (Rename + Create + Write) and
//     remove-then-recreate sequences, in addition to plain Write events.
//     Pure removes / pure renames must log and stop firing reloads (no
//     crash, no spurious fires).
//
// All synchronization uses the Wave 4 G35 channel-based reload signal
// pattern (reloadSignal in watcher_coverage_test.go).

// captureLogger returns a slog.Logger that writes JSON to the returned
// goroutine-safe buffer, plus the buffer for assertion. JSON keeps
// assertions stable across slog formatting tweaks; the safeBuffer
// serializes writes from the watcher goroutine and reads from the test
// goroutine so -race stays clean.
func captureLogger() (*slog.Logger, *safeBuffer) {
	buf := &safeBuffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), buf
}

// TestG20_WatchSkipsNonFileSources observable: given a mixed list of
// sources, only the real file path is registered in w.files and the
// non-file sources are reported via a warning log. Constructor must NOT
// return an error for the non-file sources.
func TestG20_WatchSkipsNonFileSources(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.yaml")
	require.NoError(t, os.WriteFile(real, []byte("k: v"), 0644))

	logger, buf := captureLogger()

	sources := []string{
		"http://example.com/cfg.yaml",
		"https://example.com/cfg.yaml",
		"environment:APP",
		"s3://bucket/key",
		"ssm:/some/prefix",
		"gs://bucket/object",
		"azure://container/blob",
		"ibmcos://bucket/key",
		"git:repo@main/path",
		"consul://localhost:8500/key",
		"vault://secret/data",
		real,
	}

	w, err := New(sources, func() error { return nil }, logger)
	require.NoError(t, err, "non-file sources must not produce a constructor error")
	require.NotNil(t, w)
	defer w.Stop()

	// Only the real file path is registered.
	assert.Len(t, w.files, 1, "only real file should be registered with fsnotify")
	assert.Contains(t, w.files, real)

	// Each non-file source produced a warning log entry mentioning the
	// source string. We don't pin the exact log message, just the
	// observable that the source identifier appears in a warn-level
	// record.
	logOutput := buf.String()
	for _, s := range sources {
		if s == real {
			continue
		}
		assert.Contains(t, logOutput, s,
			"expected warning log to mention skipped source %q", s)
	}
	// And the real file must NOT appear in any "skipping" warning line.
	for _, line := range strings.Split(logOutput, "\n") {
		if strings.Contains(line, "skipping non-file source") {
			assert.NotContains(t, line, real,
				"real file path must not be reported as skipped")
		}
	}
}

// TestG20_AtomicSaveTriggersReload observable: an atomic-save (write
// tmpfile, rename over target) fires reload at least once. Editors like
// vim / VS Code default to this pattern; without Rename/Create handling
// the watcher would miss saves entirely.
func TestG20_AtomicSaveTriggersReload(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("v1"), 0644))

	logger, _ := captureLogger()
	reloaded, count, reloadFn := reloadSignal(8)

	w, err := New([]string{target}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	// Atomic-save: write the new content to a sibling tmpfile, then
	// rename it over the target. This is the canonical "safe write"
	// sequence used by vim's :w and many text editors.
	tmp := filepath.Join(dir, "config.yaml.tmp")
	require.NoError(t, os.WriteFile(tmp, []byte("v2"), 0644))
	require.NoError(t, os.Rename(tmp, target))

	// Block on the channel signal — this is the primary wait. The
	// time.After deadline is only a hang guard.
	select {
	case <-reloaded:
		// Got at least one reload from the rename-over sequence.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for atomic-save reload")
	}

	assert.GreaterOrEqual(t, atomic.LoadInt64(count), int64(1),
		"atomic-save sequence must fire at least one reload")
}

// TestG20_RemoveAndRecreateTriggersReload observable: removing the
// watched file and recreating it at the same path re-arms reload. The
// initial Remove must NOT crash and must NOT prevent the subsequent
// Create+Write from firing reload.
func TestG20_RemoveAndRecreateTriggersReload(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("v1"), 0644))

	logger, buf := captureLogger()
	reloaded, count, reloadFn := reloadSignal(16)

	w, err := New([]string{target}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	// Remove the watched file. The directory watch survives.
	require.NoError(t, os.Remove(target))

	// Wait until the watcher has observed the removal — the log line
	// is the deterministic post-condition. We poll the buffer briefly.
	deadline := time.After(2 * time.Second)
	for !strings.Contains(buf.String(), "watched file removed") {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for remove-log observable")
		case <-time.After(20 * time.Millisecond):
		}
	}

	// Drain any reload signals fired during the remove phase. The
	// remove path itself MUST NOT fire reload, but Create-on-recreate
	// will, and we want a clean post-recreate count.
	beforeRecreate := atomic.LoadInt64(count)

	// Recreate at the same path with new content. This produces a
	// Create event followed by a Write event; both should fire reload.
	require.NoError(t, os.WriteFile(target, []byte("v2"), 0644))

	select {
	case <-reloaded:
		// Got reload after recreate.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for recreate-triggered reload")
	}

	afterRecreate := atomic.LoadInt64(count)
	assert.Greater(t, afterRecreate, beforeRecreate,
		"recreate must fire at least one additional reload")
}

// TestG20_RemoveOnly_LogsAndStopsWatching observable: a pure Remove
// (no recreate) produces a single info log mentioning "removed" and
// does NOT fire spurious reloads. The watcher must remain alive (Stop
// must succeed without panic).
func TestG20_RemoveOnly_LogsAndStopsWatching(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("v1"), 0644))

	logger, buf := captureLogger()
	_, count, reloadFn := reloadSignal(8)

	w, err := New([]string{target}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	require.NoError(t, os.Remove(target))

	// Wait deterministically for the remove-log post-condition.
	deadline := time.After(2 * time.Second)
	for !strings.Contains(buf.String(), "watched file removed") {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for remove log")
		case <-time.After(20 * time.Millisecond):
		}
	}

	// Quiet period: confirm no further reload fires occur after the
	// remove. Bound the wait but keep it modest — a spurious reload
	// loop would manifest as monotonic count growth.
	c1 := atomic.LoadInt64(count)
	time.Sleep(150 * time.Millisecond)
	c2 := atomic.LoadInt64(count)
	assert.Equal(t, c1, c2,
		"pure remove must not fire reload (got %d -> %d)", c1, c2)

	// Watcher must still be stoppable cleanly (no panic).
	w.Stop()
}

// TestG20_RenameOnly_LogsAndStopsWatching observable: a pure Rename
// (move the watched file to a non-watched path, no recreate at the
// original path) produces a "renamed away" info log and does NOT fire
// reload. Without Rename handling the loop would silently miss the
// state change.
func TestG20_RenameOnly_LogsAndStopsWatching(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	moved := filepath.Join(dir, "config.yaml.moved")
	require.NoError(t, os.WriteFile(target, []byte("v1"), 0644))

	logger, buf := captureLogger()
	_, count, reloadFn := reloadSignal(8)

	w, err := New([]string{target}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	require.NoError(t, os.Rename(target, moved))

	// Wait deterministically for the rename-log post-condition.
	deadline := time.After(2 * time.Second)
	for !strings.Contains(buf.String(), "watched file renamed away") {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for rename log")
		case <-time.After(20 * time.Millisecond):
		}
	}

	// Quiet period: a pure rename of the watched target away must not
	// fire reload. (The destination path is NOT in w.files so its
	// Create event is filtered.)
	c1 := atomic.LoadInt64(count)
	time.Sleep(150 * time.Millisecond)
	c2 := atomic.LoadInt64(count)
	assert.Equal(t, c1, c2,
		"pure rename-away must not fire reload (got %d -> %d)", c1, c2)
}
