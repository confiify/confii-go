package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionManager_Defaults(t *testing.T) {
	vm := NewVersionManager("", 0)
	assert.Equal(t, ".confii/versions", vm.storagePath)
	assert.Equal(t, 100, vm.maxVersions)
}

func TestVersionManager_SaveVersionWithNilMetadata(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v, err := vm.SaveVersion(map[string]any{"key": "value"}, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, v.VersionID)
	assert.Nil(t, v.Metadata)
	assert.Equal(t, "value", v.Config["key"])
}

func TestVersionManager_SaveVersionImmutability(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	original := map[string]any{"key": "original"}
	v, err := vm.SaveVersion(original, nil)
	require.NoError(t, err)

	// Mutate the original map.
	original["key"] = "mutated"

	// The saved version should still have the original value.
	assert.Equal(t, "original", v.Config["key"])
}

func TestVersionManager_SaveVersionPersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v, err := vm.SaveVersion(map[string]any{"persisted": true}, nil)
	require.NoError(t, err)

	// Verify the file exists on disk.
	path := filepath.Join(dir, v.VersionID+".json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var loaded Version
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, v.VersionID, loaded.VersionID)
	assert.Equal(t, true, loaded.Config["persisted"])
}

func TestVersionManager_GetVersionFromDisk(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v, err := vm.SaveVersion(map[string]any{"disk": "load"}, nil)
	require.NoError(t, err)

	// Create a fresh manager that has no in-memory versions.
	vm2 := NewVersionManager(dir, 100)
	got := vm2.GetVersion(v.VersionID)
	require.NotNil(t, got)
	assert.Equal(t, v.VersionID, got.VersionID)
	assert.Equal(t, "load", got.Config["disk"])
}

func TestVersionManager_GetVersionInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	// Write an invalid JSON file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "badjson.json"), []byte("{invalid"), 0644))

	got := vm.GetVersion("badjson")
	assert.Nil(t, got)
}

func TestVersionManager_ListVersionsOrder(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	_, _ = vm.SaveVersion(map[string]any{"v": 1}, nil)
	time.Sleep(10 * time.Millisecond) // ensure different timestamps
	_, _ = vm.SaveVersion(map[string]any{"v": 2}, nil)
	time.Sleep(10 * time.Millisecond)
	_, _ = vm.SaveVersion(map[string]any{"v": 3}, nil)

	versions := vm.ListVersions()
	require.Len(t, versions, 3)
	// Newest first: timestamps should be descending.
	assert.True(t, versions[0].Timestamp >= versions[1].Timestamp)
	assert.True(t, versions[1].Timestamp >= versions[2].Timestamp)
}

func TestVersionManager_ListVersionsEmpty(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	versions := vm.ListVersions()
	assert.Empty(t, versions)
}

func TestVersionManager_LatestVersionEmpty(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	latest := vm.LatestVersion()
	assert.Nil(t, latest)
}

func TestVersionManager_ScanDiskLoadsVersions(t *testing.T) {
	dir := t.TempDir()

	// Create version files on disk directly.
	for i := 0; i < 3; i++ {
		v := &Version{
			VersionID: "scantest" + string(rune('0'+i)),
			Config:    map[string]any{"index": i},
			Timestamp: float64(1000 + i),
			DateTime:  "2026-01-01T00:00:00Z",
		}
		data, _ := json.MarshalIndent(v, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, v.VersionID+".json"), data, 0644)
	}

	// Create a fresh manager and list to trigger scanDisk.
	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()
	assert.Len(t, versions, 3)
}

func TestVersionManager_ScanDiskSkipsNonJSON(t *testing.T) {
	dir := t.TempDir()

	// Write a non-JSON file and a valid JSON version.
	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not json"), 0644)
	v := &Version{
		VersionID: "valid123",
		Config:    map[string]any{"ok": true},
		Timestamp: 1000,
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "valid123.json"), data, 0644)

	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()
	assert.Len(t, versions, 1)
	assert.Equal(t, "valid123", versions[0].VersionID)
}

func TestVersionManager_ScanDiskSkipsInvalidJSON(t *testing.T) {
	dir := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{invalid json"), 0644)

	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()
	assert.Empty(t, versions)
}

func TestVersionManager_ScanDiskNonexistentDir(t *testing.T) {
	vm := NewVersionManager("/nonexistent/path/versions", 100)
	versions := vm.ListVersions()
	assert.Empty(t, versions)
}

func TestVersionManager_EvictRemovesOldest(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 2)

	v1, _ := vm.SaveVersion(map[string]any{"v": 1}, nil)
	time.Sleep(1100 * time.Millisecond) // ensure distinct Unix() timestamps
	_, _ = vm.SaveVersion(map[string]any{"v": 2}, nil)
	time.Sleep(1100 * time.Millisecond)
	_, _ = vm.SaveVersion(map[string]any{"v": 3}, nil)

	// v1 should have been evicted (oldest by timestamp).
	got := vm.GetVersion(v1.VersionID)
	assert.Nil(t, got)

	// Disk file should also be removed.
	_, err := os.Stat(filepath.Join(dir, v1.VersionID+".json"))
	assert.True(t, os.IsNotExist(err))

	versions := vm.ListVersions()
	assert.Len(t, versions, 2)
}

func TestVersionManager_DiffVersions_Modified(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v1, _ := vm.SaveVersion(map[string]any{"host": "localhost", "port": 5432}, nil)
	v2, _ := vm.SaveVersion(map[string]any{"host": "prod-db", "port": 5432}, nil)

	diffs, err := vm.DiffVersions(v1.VersionID, v2.VersionID)
	require.NoError(t, err)

	// host changed, port stayed the same.
	var hostDiff map[string]any
	for _, d := range diffs {
		if d["path"] == "host" {
			hostDiff = d
		}
	}
	require.NotNil(t, hostDiff)
	assert.Equal(t, "modified", hostDiff["type"])
	assert.Equal(t, "localhost", hostDiff["old_value"])
	assert.Equal(t, "prod-db", hostDiff["new_value"])
}

func TestVersionManager_DiffVersions_AddedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v1, _ := vm.SaveVersion(map[string]any{"old_key": "value"}, nil)
	v2, _ := vm.SaveVersion(map[string]any{"new_key": "value"}, nil)

	diffs, err := vm.DiffVersions(v1.VersionID, v2.VersionID)
	require.NoError(t, err)

	types := make(map[string]string)
	for _, d := range diffs {
		types[d["path"].(string)] = d["type"].(string)
	}
	assert.Equal(t, "removed", types["old_key"])
	assert.Equal(t, "added", types["new_key"])
}

func TestVersionManager_DiffVersions_NestedMaps(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v1, _ := vm.SaveVersion(map[string]any{
		"db": map[string]any{"host": "localhost", "port": float64(5432)},
	}, nil)
	v2, _ := vm.SaveVersion(map[string]any{
		"db": map[string]any{"host": "prod", "port": float64(5432)},
	}, nil)

	diffs, err := vm.DiffVersions(v1.VersionID, v2.VersionID)
	require.NoError(t, err)

	var hostDiff map[string]any
	for _, d := range diffs {
		if d["path"] == "db.host" {
			hostDiff = d
		}
	}
	require.NotNil(t, hostDiff)
	assert.Equal(t, "modified", hostDiff["type"])
}

func TestVersionManager_DiffVersions_NoDifferences(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v1, _ := vm.SaveVersion(map[string]any{"key": "same"}, nil)
	v2, _ := vm.SaveVersion(map[string]any{"key": "same"}, nil)

	diffs, err := vm.DiffVersions(v1.VersionID, v2.VersionID)
	require.NoError(t, err)
	assert.Empty(t, diffs)
}

func TestVersionManager_ScanDiskWithUnreadableFile(t *testing.T) {
	dir := t.TempDir()

	// Create a valid version file and an unreadable one.
	v := &Version{
		VersionID: "readable",
		Config:    map[string]any{"ok": true},
		Timestamp: 1000,
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "readable.json"), data, 0644)

	// Create unreadable file.
	unreadablePath := filepath.Join(dir, "unreadable.json")
	_ = os.WriteFile(unreadablePath, []byte(`{"version_id":"unreadable"}`), 0000)

	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()
	// The readable version should be loaded; the unreadable one should be skipped.
	assert.GreaterOrEqual(t, len(versions), 1)

	// Cleanup: make the file writable again so TempDir cleanup succeeds.
	_ = os.Chmod(unreadablePath, 0644)
}

func TestVersionManager_DiffVersions_NotFound(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v1, _ := vm.SaveVersion(map[string]any{"key": "value"}, nil)

	_, err := vm.DiffVersions(v1.VersionID, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")

	_, err = vm.DiffVersions("nonexistent", v1.VersionID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}
