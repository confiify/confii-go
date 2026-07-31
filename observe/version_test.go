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
