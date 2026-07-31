// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/confiify/confii-go/v2/diff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionManager_Defaults(t *testing.T) {
	vm := NewVersionManager("", 0)

	assert.Equal(t, "", vm.storagePath)
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
	metadata := map[string]any{"actor": "original"}
	v, err := vm.SaveVersion(original, metadata)
	require.NoError(t, err)

	original["key"] = "mutated"
	metadata["actor"] = "mutated"

	assert.Equal(t, "original", v.Config["key"])
	assert.Equal(t, "original", v.Metadata["actor"])
}

func TestVersionManager_SaveVersionPersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v, err := vm.SaveVersion(map[string]any{"persisted": true}, nil)
	require.NoError(t, err)

	path := filepath.Join(dir, v.VersionID+".json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}

	var loaded Version
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, v.VersionID, loaded.VersionID)
	assert.True(t, loaded.Timestamp.Equal(v.Timestamp))
	assert.Equal(t, true, loaded.Config["persisted"])
}

func TestVersionManager_GetVersionRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside.json")
	require.NoError(t, os.WriteFile(outside, []byte(`{"VersionID":"outside"}`), 0600))

	vm := NewVersionManager(dir, 100)
	assert.Nil(t, vm.GetVersion("../outside"))
}

func TestVersionManager_GetVersionRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	const versionID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	outside := filepath.Join(filepath.Dir(dir), "outside-version.json")
	record := &Version{VersionID: versionID, Config: map[string]any{"escaped": true}}
	data, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(outside, data, 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, versionID+".json")))

	vm := NewVersionManager(dir, 100)
	assert.Nil(t, vm.GetVersion(versionID))
	assert.Empty(t, vm.ListVersions())
}

func TestVersionManager_GetVersionFromDisk(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v, err := vm.SaveVersion(map[string]any{"disk": "load"}, nil)
	require.NoError(t, err)

	vm2 := NewVersionManager(dir, 100)
	got := vm2.GetVersion(v.VersionID)
	require.NotNil(t, got)
	assert.Equal(t, v.VersionID, got.VersionID)
	assert.Equal(t, "load", got.Config["disk"])
}

func TestVersionManager_GetVersionInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	const versionID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	require.NoError(t, os.WriteFile(filepath.Join(dir, versionID+".json"), []byte("{invalid"), 0644))

	got := vm.GetVersion(versionID)
	assert.Nil(t, got)
}

func TestVersionManager_ListVersionsOrder(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	_, _ = vm.SaveVersion(map[string]any{"v": 1}, nil)
	time.Sleep(10 * time.Millisecond)
	_, _ = vm.SaveVersion(map[string]any{"v": 2}, nil)
	time.Sleep(10 * time.Millisecond)
	_, _ = vm.SaveVersion(map[string]any{"v": 3}, nil)

	versions := vm.ListVersions()
	require.Len(t, versions, 3)

	assert.False(t, versions[0].Timestamp.Before(versions[1].Timestamp))
	assert.False(t, versions[1].Timestamp.Before(versions[2].Timestamp))
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

	for i := 0; i < 3; i++ {
		v := &Version{
			VersionID: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW", "01ARZ3NDEKTSV4RRFFQ69G5FAX"}[i],
			Config:    map[string]any{"index": i},
			Timestamp: time.Unix(int64(1000+i), 0).UTC(),
		}
		data, _ := json.MarshalIndent(v, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, v.VersionID+".json"), data, 0644)
	}

	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()
	assert.Len(t, versions, 3)
}

func TestVersionManager_ScanDiskSkipsNonJSON(t *testing.T) {
	dir := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not json"), 0644)
	const versionID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	v := &Version{
		VersionID: versionID,
		Config:    map[string]any{"ok": true},
		Timestamp: time.Unix(1000, 0).UTC(),
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, versionID+".json"), data, 0644)

	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()
	assert.Len(t, versions, 1)
	assert.Equal(t, versionID, versions[0].VersionID)
}

func TestVersionManager_ScanDiskSkipsInvalidJSON(t *testing.T) {
	dir := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir, "01ARZ3NDEKTSV4RRFFQ69G5FAV.json"), []byte("{invalid json"), 0644)

	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()
	assert.Empty(t, versions)
}

func TestVersionManager_ScanDiskSkipsMismatchedVersionID(t *testing.T) {
	dir := t.TempDir()
	record := &Version{VersionID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Config: map[string]any{"ok": false}}
	data, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "01ARZ3NDEKTSV4RRFFQ69G5FAW.json"), data, 0600))

	vm := NewVersionManager(dir, 100)
	assert.Empty(t, vm.ListVersions())
}

func TestVersionManager_ScanDiskNonexistentDir(t *testing.T) {
	vm := NewVersionManager("/nonexistent/path/versions", 100)
	versions := vm.ListVersions()
	assert.Empty(t, versions)
}

func TestVersionManager_EvictRemovesOldest(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 2)

	// Timestamps are now nanosecond-monotonic, so back-to-back saves still
	// produce strictly increasing values. No sleeps needed.
	v1, _ := vm.SaveVersion(map[string]any{"v": 1}, nil)
	_, _ = vm.SaveVersion(map[string]any{"v": 2}, nil)
	_, _ = vm.SaveVersion(map[string]any{"v": 3}, nil)

	got := vm.GetVersion(v1.VersionID)
	assert.Nil(t, got)

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

	var hostDiff *diff.ConfigDiff
	for _, d := range diffs {
		if d.Path == "host" {
			copy := d
			hostDiff = &copy
		}
	}
	require.NotNil(t, hostDiff)
	assert.Equal(t, diff.Modified, hostDiff.Type)
	assert.Equal(t, "localhost", hostDiff.OldValue)
	assert.Equal(t, "prod-db", hostDiff.NewValue)
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
		types[d.Path] = string(d.Type)
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

	var hostDiff *diff.ConfigDiff
	for _, d := range diffs {
		if d.Path == "db" {
			for _, nested := range d.NestedDiffs {
				if nested.Path == "db.host" {
					copy := nested
					hostDiff = &copy
				}
			}
		}
	}
	require.NotNil(t, hostDiff)
	assert.Equal(t, diff.Modified, hostDiff.Type)
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

	v := &Version{
		VersionID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Config:    map[string]any{"ok": true},
		Timestamp: time.Unix(1000, 0).UTC(),
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, v.VersionID+".json"), data, 0644)

	unreadablePath := filepath.Join(dir, "01ARZ3NDEKTSV4RRFFQ69G5FAW.json")
	_ = os.WriteFile(unreadablePath, []byte(`{"version_id":"01ARZ3NDEKTSV4RRFFQ69G5FAW"}`), 0000)

	vm := NewVersionManager(dir, 100)
	versions := vm.ListVersions()

	assert.GreaterOrEqual(t, len(versions), 1)

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
