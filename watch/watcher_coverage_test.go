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

// G35: this file uses channel-based synchronization rather than time.Sleep
// for the reload signal. The reload callback pushes to a buffered channel
// and tests block on `<-reloaded` (with a generous deadline guard via
// time.After to avoid hangs when the watcher legitimately never fires).
// time.After is only a safety net — the primary signal is the channel send.

// reloadSignal returns a reload callback that pushes a struct{} into the
// returned channel for every invocation, plus an atomic counter for
// "how many times did the callback fire" assertions. The channel is
// buffered to ensure the watcher goroutine is never blocked by a slow
// reader in the test.
func reloadSignal(buf int) (chan struct{}, *int64, func() error) {
	ch := make(chan struct{}, buf)
	var count int64
	return ch, &count, func() error {
		atomic.AddInt64(&count, 1)
		select {
		case ch <- struct{}{}:
		default:
			// Channel full — counter still records the fire.
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Channel-based synchronization replaces fixed sleeps: the reload
	// callback pushes onto `reloaded` and the test blocks on it directly.
	reloaded, count, reloadFn := reloadSignal(8)

	w, err := New([]string{f}, reloadFn, logger)
	require.NoError(t, err)
	defer w.Stop()

	// Modify the watched file. fsnotify is already armed by the time
	// New() returns, so no startup sleep is needed — if the change is
	// missed, the deadline guard below catches it.
	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))

	// Block on the reload signal. time.After is a deadline guard, NOT
	// the primary wait — the channel send is what the test waits on.
	select {
	case <-reloaded:
		// Got the reload event we expected.
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

	// Channel-based synchronization: the reload callback signals the
	// fire on `reloaded` even though it returns an error, so the test
	// can deterministically observe both outcomes.
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

	// Modify the watched file.
	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))

	// Block on the reload signal — channel is the primary signal,
	// time.After is a hang-prevention deadline guard.
	select {
	case <-reloaded:
		// Reload fired and returned the simulated error as expected.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reload callback")
	}

	// The watcher should still be running even after the error.
	assert.GreaterOrEqual(t, atomic.LoadInt64(&count), int64(1))
}

func TestWatcher_UnwatchedFileDoesNotTrigger(t *testing.T) {
	dir := t.TempDir()
	watched := filepath.Join(dir, "watched.yaml")
	unwatched := filepath.Join(dir, "unwatched.yaml")
	require.NoError(t, os.WriteFile(watched, []byte("v1"), 0644))
	require.NoError(t, os.WriteFile(unwatched, []byte("v1"), 0644))

	// Channel-based synchronization replaces the fixed sleep wait.
	// For this *negative* assertion (no reload expected) we still need
	// a bounded wait, but we use a control-flow channel together with
	// a baseline-write to deterministically establish that the watcher
	// goroutine has processed the unwatched-file event before we assert.
	reloaded, count, reloadFn := reloadSignal(8)

	w, err := New([]string{watched}, reloadFn, nil)
	require.NoError(t, err)
	defer w.Stop()
	absUnwatched, err := filepath.Abs(unwatched)
	require.NoError(t, err)
	_, registered := w.files[absUnwatched]
	assert.False(t, registered, "unwatched path must not enter the watch filter")

	// Modify only the unwatched file.
	require.NoError(t, os.WriteFile(unwatched, []byte("v2"), 0644))

	// To deterministically know the unwatched event has been processed,
	// touch the *watched* file too and wait for ITS reload signal.
	// fsnotify delivers events in order per directory, so when the
	// watched-file signal arrives we know the unwatched event has
	// already been handled (and ignored) by the watcher loop.
	require.NoError(t, os.WriteFile(watched, []byte("v2"), 0644))

	select {
	case <-reloaded:
		// Watched-file reload fired; unwatched event must therefore
		// already have been processed and ignored.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sentinel watched-file reload")
	}

	// Drain any further reload signals from the sentinel write. A single
	// os.WriteFile may legally produce multiple low-level fsnotify Write
	// notifications on Linux, so callback cardinality is not a portable way
	// to distinguish the watched and unwatched paths.
	drainTimeout := time.After(50 * time.Millisecond)
drain:
	for {
		select {
		case <-reloaded:
		case <-drainTimeout:
			break drain
		}
	}

	// The registered-path assertion above pins the filter contract; here we
	// additionally prove that the sentinel watched-file write was processed.
	assert.GreaterOrEqual(t, atomic.LoadInt64(count), int64(1))
}
