// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package watch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reloadSignal(buf int) (chan struct{}, *int64, func() error) {
	ch := make(chan struct{}, buf)
	var count int64
	return ch, &count, func() error {
		atomic.AddInt64(&count, 1)
		select {
		case ch <- struct{}{}:
		default:
		}
		return nil
	}
}

func TestWatcher_ReplaceFilesUpdatesActiveSet(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.yaml")
	b := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(a, []byte("x: 1"), 0644))
	require.NoError(t, os.WriteFile(b, []byte("y: 2"), 0644))

	w, err := New([]string{a}, func() error { return nil }, nil)
	require.NoError(t, err)
	defer w.Stop()
	require.Equal(t, []string{a}, w.Files(),
		"a freshly constructed watcher reports its initial trigger file")

	require.NoError(t, w.ReplaceFiles([]string{b}))
	require.Equal(t, []string{b}, w.Files(),
		"ReplaceFiles must atomically update the active trigger set reported by Files")
}

func TestWatcher_DebounceCoalescesBurstAtTrailingEdge(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte("value: 1"), 0o600))
	var reloads atomic.Int64
	reloaded := make(chan struct{}, 1)
	logger, logs := captureLogger()
	const debounce = time.Second
	w, err := New([]string{file}, func() error {
		reloads.Add(1)
		reloaded <- struct{}{}
		return nil
	}, logger, WithDebounce(debounce))
	require.NoError(t, err)
	defer w.Stop()

	for index := range 5 {
		require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf("value: %d", index+2)), 0o600))
	}

	// Wait until the watcher has observed multiple events from the burst. This
	// proves that the assertion exercises coalescing without making the burst
	// duration depend on filesystem speed or race-detector scheduling.
	deadline := time.Now().Add(2 * time.Second)
	observedEvents := 0
	for observedEvents < 2 && time.Now().Before(deadline) {
		observedEvents = strings.Count(logs.String(), "config file changed, scheduling reload")
		time.Sleep(10 * time.Millisecond)
	}
	require.GreaterOrEqual(t, observedEvents, 2, "test burst must produce multiple watcher events")
	assert.Zero(t, reloads.Load(), "trailing-edge debounce must not fire while the burst is being observed")

	select {
	case <-reloaded:
	case <-time.After(2 * debounce):
		t.Fatal("timed out waiting for debounced reload")
	}
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(1), reloads.Load(), "one burst must publish one reload")
}

func TestWatcher_ZeroDebounceReloadsImmediatelyAndStopCancelsPending(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte("value: 1"), 0o600))

	immediate := make(chan struct{}, 1)
	w, err := New([]string{file}, func() error {
		immediate <- struct{}{}
		return nil
	}, nil, WithDebounce(0))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte("value: 2"), 0o600))
	select {
	case <-immediate:
	case <-time.After(time.Second):
		t.Fatal("zero debounce did not reload immediately")
	}
	w.Stop()

	var delayed atomic.Int64
	logger, logs := captureLogger()
	w, err = New([]string{file}, func() error {
		delayed.Add(1)
		return nil
	}, logger, WithDebounce(200*time.Millisecond))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte("value: 3"), 0o600))
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.String(), "config file changed") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.Contains(t, logs.String(), "config file changed", "pending reload must be scheduled before Stop")
	w.Stop()
	time.Sleep(250 * time.Millisecond)
	assert.Zero(t, delayed.Load(), "Stop must cancel a pending trailing-edge reload")
}

func TestWatcher_RejectsInvalidOptions(t *testing.T) {
	watcher, err := New(nil, nil, nil, WithDebounce(-time.Millisecond))
	require.Error(t, err)
	assert.Nil(t, watcher)

	watcher, err = New(nil, nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, watcher)

	watcher, err = New(nil, nil, nil, func(*options) { panic("boom") })
	require.Error(t, err)
	assert.Nil(t, watcher)
}

func TestNew_ValidFiles(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("key: value"), 0644))

	w, err := New([]string{f}, func() error { return nil }, nil)
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Stop()

	assert.NotNil(t, w.watcher)
	assert.Contains(t, w.files, f)
}

func TestNewWithContext_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("key:value"), 0644))

	ctx, cancel := context.WithCancel(context.Background())
	w, err := NewWithContext(ctx, []string{f}, func(callbackCtx context.Context) error {
		return callbackCtx.Err()
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, w)

	cancel()
	select {
	case <-w.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("watcher context was not canceled")
	}
	w.Stop()

	nilWatcher, err := NewWithContext(nil, []string{f}, func(context.Context) error { return nil }, nil) //nolint:staticcheck // verifies the nil-context fallback
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, nilWatcher)
}

func TestNew_NonExistentDirectory(t *testing.T) {

	w, err := New([]string{"/nonexistent_dir_confii_test/config.yaml"}, func() error { return nil }, nil)
	assert.Error(t, err)
	assert.Nil(t, w)
}

func TestStop_Idempotent(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("data"), 0644))

	w, err := New([]string{f}, func() error { return nil }, nil)
	require.NoError(t, err)

	w.Stop()
	w.Stop()
}

func TestWatcher_FileChangeTriggersReload(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v1"), 0644))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	reloaded, count, reloadFn := reloadSignal(8)

	w, err := New([]string{f}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))

	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reload callback")
	}

	assert.GreaterOrEqual(t, atomic.LoadInt64(count), int64(1))
}

func TestWatcher_ReloadFuncReturnsError(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v1"), 0644))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	reloaded := make(chan struct{}, 8)
	var count int64

	w, err := New([]string{f}, func() error {
		atomic.AddInt64(&count, 1)
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return fmt.Errorf("simulated reload error")
	}, logger)
	require.NoError(t, err)
	defer w.Stop()

	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))

	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reload callback")
	}

	assert.GreaterOrEqual(t, atomic.LoadInt64(&count), int64(1))
}

func TestWatcher_UnwatchedFileDoesNotTrigger(t *testing.T) {
	dir := t.TempDir()
	watched := filepath.Join(dir, "watched.yaml")
	unwatched := filepath.Join(dir, "unwatched.yaml")
	require.NoError(t, os.WriteFile(watched, []byte("v1"), 0644))
	require.NoError(t, os.WriteFile(unwatched, []byte("v1"), 0644))

	reloaded, count, reloadFn := reloadSignal(8)

	w, err := New([]string{watched}, reloadFn, nil)
	require.NoError(t, err)
	defer w.Stop()
	absUnwatched, err := filepath.Abs(unwatched)
	require.NoError(t, err)
	_, registered := w.files[absUnwatched]
	assert.False(t, registered, "unwatched path must not enter the watch filter")

	require.NoError(t, os.WriteFile(unwatched, []byte("v2"), 0644))

	require.NoError(t, os.WriteFile(watched, []byte("v2"), 0644))

	select {
	case <-reloaded:

	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sentinel watched-file reload")
	}

	drainTimeout := time.After(50 * time.Millisecond)
drain:
	for {
		select {
		case <-reloaded:
		case <-drainTimeout:
			break drain
		}
	}

	assert.GreaterOrEqual(t, atomic.LoadInt64(count), int64(1))
}
