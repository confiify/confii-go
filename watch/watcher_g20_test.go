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

func captureLogger() (*slog.Logger, *safeBuffer) {
	buf := &safeBuffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), buf
}

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

	assert.Len(t, w.files, 1, "only real file should be registered with fsnotify")
	assert.Contains(t, w.files, real)

	logOutput := buf.String()
	for _, s := range sources {
		if s == real {
			continue
		}
		assert.Contains(t, logOutput, s,
			"expected warning log to mention skipped source %q", s)
	}

	for _, line := range strings.Split(logOutput, "\n") {
		if strings.Contains(line, "skipping non-file source") {
			assert.NotContains(t, line, real,
				"real file path must not be reported as skipped")
		}
	}
}

func TestG20_AtomicSaveTriggersReload(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("v1"), 0644))

	logger, _ := captureLogger()
	reloaded, count, reloadFn := reloadSignal(8)

	w, err := New([]string{target}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	tmp := filepath.Join(dir, "config.yaml.tmp")
	require.NoError(t, os.WriteFile(tmp, []byte("v2"), 0644))
	require.NoError(t, os.Rename(tmp, target))

	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for atomic-save reload")
	}

	assert.GreaterOrEqual(t, atomic.LoadInt64(count), int64(1),
		"atomic-save sequence must fire at least one reload")
}

func TestG20_RemoveAndRecreateTriggersReload(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("v1"), 0644))

	logger, buf := captureLogger()
	reloaded, count, reloadFn := reloadSignal(16)

	w, err := New([]string{target}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	require.NoError(t, os.Remove(target))

	deadline := time.After(2 * time.Second)
	for !strings.Contains(buf.String(), "watched file removed") {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for remove-log observable")
		case <-time.After(20 * time.Millisecond):
		}
	}

	beforeRecreate := atomic.LoadInt64(count)

	require.NoError(t, os.WriteFile(target, []byte("v2"), 0644))

	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for recreate-triggered reload")
	}

	afterRecreate := atomic.LoadInt64(count)
	assert.Greater(t, afterRecreate, beforeRecreate,
		"recreate must fire at least one additional reload")
}

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

	deadline := time.After(2 * time.Second)
	for !strings.Contains(buf.String(), "watched file removed") {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for remove log")
		case <-time.After(20 * time.Millisecond):
		}
	}

	c1 := atomic.LoadInt64(count)
	time.Sleep(150 * time.Millisecond)
	c2 := atomic.LoadInt64(count)
	assert.Equal(t, c1, c2,
		"pure remove must not fire reload (got %d -> %d)", c1, c2)

	w.Stop()
}

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

	deadline := time.After(2 * time.Second)
	for !strings.Contains(buf.String(), "watched file renamed away") {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for rename log")
		case <-time.After(20 * time.Millisecond):
		}
	}

	c1 := atomic.LoadInt64(count)
	time.Sleep(150 * time.Millisecond)
	c2 := atomic.LoadInt64(count)
	assert.Equal(t, c1, c2,
		"pure rename-away must not fire reload (got %d -> %d)", c1, c2)
}
