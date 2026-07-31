// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionManager_SaveAndGet(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	config := map[string]any{"database": map[string]any{"host": "localhost"}}
	v, err := vm.SaveVersion(config, map[string]any{"author": "test"})
	require.NoError(t, err)
	assert.NotEmpty(t, v.VersionID)
	assert.True(t, validVersionID(v.VersionID))
	assert.False(t, v.Timestamp.IsZero())
	assert.Equal(t, "localhost", v.Config["database"].(map[string]any)["host"])
	assert.Equal(t, "test", v.Metadata["author"])

	got := vm.GetVersion(v.VersionID)
	require.NotNil(t, got)
	assert.Equal(t, v.VersionID, got.VersionID)
}

func TestVersionManager_ReturnsDetachedRecords(t *testing.T) {
	vm := NewVersionManager("", 3)
	saved, err := vm.SaveVersionWithSensitivePaths(
		map[string]any{"database": map[string]any{"password": "secret"}},
		map[string]any{"author": "test"},
		[]string{"database.password"},
	)
	require.NoError(t, err)
	saved.Config["database"].(map[string]any)["password"] = "changed"
	saved.Metadata["author"] = "changed"
	saved.SensitivePaths[0] = "changed"

	stored := vm.GetVersion(saved.VersionID)
	require.NotNil(t, stored)
	assert.Equal(t, "secret", stored.Config["database"].(map[string]any)["password"])
	assert.Equal(t, "test", stored.Metadata["author"])
	assert.Equal(t, []string{"database.password"}, stored.SensitivePaths)

	listed := vm.ListVersions()
	listed[0].Config["database"].(map[string]any)["password"] = "listed-change"
	assert.Equal(t, "secret", vm.LatestVersion().Config["database"].(map[string]any)["password"])
}

func TestVersionManager_ListVersions(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	v1, _ := vm.SaveVersion(map[string]any{"v": 1}, nil)
	v2, _ := vm.SaveVersion(map[string]any{"v": 2}, nil)
	v3, _ := vm.SaveVersion(map[string]any{"v": 3}, nil)

	versions := vm.ListVersions()
	assert.Len(t, versions, 3)

	assert.False(t, versions[0].Timestamp.Before(versions[1].Timestamp))
	assert.Less(t, v1.VersionID, v2.VersionID)
	assert.Less(t, v2.VersionID, v3.VersionID)
}

func TestVersionManager_Eviction(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 2)

	_, _ = vm.SaveVersion(map[string]any{"v": 1}, nil)
	_, _ = vm.SaveVersion(map[string]any{"v": 2}, nil)
	_, _ = vm.SaveVersion(map[string]any{"v": 3}, nil)

	versions := vm.ListVersions()
	assert.Len(t, versions, 2)
}

func TestVersionManager_ReconfigureAppliesRetentionImmediately(t *testing.T) {
	vm := NewVersionManager("", 3)

	first, err := vm.SaveVersion(map[string]any{"v": 1}, nil)
	require.NoError(t, err)
	_, err = vm.SaveVersion(map[string]any{"v": 2}, nil)
	require.NoError(t, err)
	latest, err := vm.SaveVersion(map[string]any{"v": 3}, nil)
	require.NoError(t, err)

	vm.Reconfigure("", 1)
	versions := vm.ListVersions()
	require.Len(t, versions, 1)
	assert.Equal(t, latest.VersionID, versions[0].VersionID)
	assert.Nil(t, vm.GetVersion(first.VersionID), "tightening retention must evict the oldest snapshots immediately")

	vm.Reconfigure("", 0)
	newest, err := vm.SaveVersion(map[string]any{"v": 4}, nil)
	require.NoError(t, err)
	versions = vm.ListVersions()
	require.Len(t, versions, 1, "a non-positive reconfiguration must retain the existing cap")
	assert.Equal(t, newest.VersionID, versions[0].VersionID)
}

func TestVersionManager_LatestVersion(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	_, _ = vm.SaveVersion(map[string]any{"v": 1}, nil)
	_, _ = vm.SaveVersion(map[string]any{"v": 2}, nil)

	latest := vm.LatestVersion()
	require.NotNil(t, latest)
}

func TestVersionManager_GetMissing(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	got := vm.GetVersion("nonexistent")
	assert.Nil(t, got)
}
