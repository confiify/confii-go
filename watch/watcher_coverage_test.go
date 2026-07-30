// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package watch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
