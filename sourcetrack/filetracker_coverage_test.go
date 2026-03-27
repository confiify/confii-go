package sourcetrack

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileTracker(t *testing.T) {
	ft := NewFileTracker()
	require.NotNil(t, ft)
	assert.NotNil(t, ft.files)
}

func TestFileTracker_Track(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("key: value"), 0644))

	ft := NewFileTracker()
	err := ft.Track(f)
	require.NoError(t, err)

	// File should be in tracked set.
	ft.mu.RLock()
	_, exists := ft.files[f]
	ft.mu.RUnlock()
	assert.True(t, exists)
}

func TestFileTracker_Track_NonExistent(t *testing.T) {
	ft := NewFileTracker()
	err := ft.Track("/tmp/does_not_exist_confii_test_file.yaml")
	assert.Error(t, err)
}

func TestFileTracker_HasChanged_Unchanged(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("key: value"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	assert.False(t, ft.HasChanged(f))
}

func TestFileTracker_HasChanged_Modified(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("key: value"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	// Ensure mtime differs (some filesystems have 1s granularity).
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(f, []byte("key: changed"), 0644))

	assert.True(t, ft.HasChanged(f))
}

func TestFileTracker_HasChanged_NewFile(t *testing.T) {
	ft := NewFileTracker()
	// An untracked file should be reported as changed.
	assert.True(t, ft.HasChanged("/some/untracked/path"))
}

func TestFileTracker_Update(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("v1"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	// Modify file.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(f, []byte("v2"), 0644))
	assert.True(t, ft.HasChanged(f))

	// Update should re-snapshot.
	require.NoError(t, ft.Update(f))
	assert.False(t, ft.HasChanged(f))
}

func TestFileTracker_GetChangedFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.yaml")
	f2 := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(f1, []byte("a"), 0644))
	require.NoError(t, os.WriteFile(f2, []byte("b"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f1))
	require.NoError(t, ft.Track(f2))

	// Modify only f1.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(f1, []byte("a-changed"), 0644))

	changed := ft.GetChangedFiles([]string{f1, f2})
	assert.Contains(t, changed, f1)
	assert.NotContains(t, changed, f2)
}

func TestFileTracker_Clear(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte("data"), 0644))

	ft := NewFileTracker()
	require.NoError(t, ft.Track(f))

	ft.Clear()

	ft.mu.RLock()
	count := len(ft.files)
	ft.mu.RUnlock()
	assert.Equal(t, 0, count)

	// After clear, the file should be reported as changed (untracked).
	assert.True(t, ft.HasChanged(f))
}
