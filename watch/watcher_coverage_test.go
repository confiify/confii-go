package watch

import (
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

func TestNew_NonExistentDirectory(t *testing.T) {
	// The directory for the file does not exist, so fsnotify.Add should fail.
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

	// Calling Stop multiple times should not panic.
	w.Stop()
	w.Stop()
}

func TestWatcher_FileChangeTriggersReload(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v1"), 0644))

	var reloadCount int64
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	w, err := New([]string{f}, func() error {
		atomic.AddInt64(&reloadCount, 1)
		return nil
	}, logger)
	require.NoError(t, err)
	defer w.Stop()

	// Give the watcher time to start.
	time.Sleep(100 * time.Millisecond)

	// Modify the watched file.
	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))

	// Wait for the reload callback to fire.
	deadline := time.After(3 * time.Second)
	for atomic.LoadInt64(&reloadCount) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reload callback")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	assert.GreaterOrEqual(t, atomic.LoadInt64(&reloadCount), int64(1))
}

func TestWatcher_ReloadFuncReturnsError(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v1"), 0644))

	var reloadCount int64
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	w, err := New([]string{f}, func() error {
		atomic.AddInt64(&reloadCount, 1)
		return fmt.Errorf("simulated reload error")
	}, logger)
	require.NoError(t, err)
	defer w.Stop()

	// Give the watcher time to start.
	time.Sleep(100 * time.Millisecond)

	// Modify the watched file.
	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))

	// Wait for the reload callback to fire.
	deadline := time.After(3 * time.Second)
	for atomic.LoadInt64(&reloadCount) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for reload callback")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// The watcher should still be running even after the error.
	assert.GreaterOrEqual(t, atomic.LoadInt64(&reloadCount), int64(1))
}

func TestWatcher_UnwatchedFileDoesNotTrigger(t *testing.T) {
	dir := t.TempDir()
	watched := filepath.Join(dir, "watched.yaml")
	unwatched := filepath.Join(dir, "unwatched.yaml")
	require.NoError(t, os.WriteFile(watched, []byte("v1"), 0644))
	require.NoError(t, os.WriteFile(unwatched, []byte("v1"), 0644))

	var reloadCount int64

	w, err := New([]string{watched}, func() error {
		atomic.AddInt64(&reloadCount, 1)
		return nil
	}, nil)
	require.NoError(t, err)
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	// Modify only the unwatched file.
	require.NoError(t, os.WriteFile(unwatched, []byte("v2"), 0644))

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int64(0), atomic.LoadInt64(&reloadCount))
}
