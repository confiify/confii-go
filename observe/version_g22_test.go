// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_ListVersions_NoMutationUnderConcurrentRead(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	for i := 0; i < 3; i++ {
		_, err := vm.SaveVersion(map[string]any{"i": i}, nil)
		require.NoError(t, err)
	}

	const writers = 3
	const readers = 5
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _ = vm.SaveVersion(map[string]any{"writer": w, "iter": i}, nil)
			}
		}()
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				vs := vm.ListVersions()
				if len(vs) > iterations*writers {
					t.Errorf("version count %d exceeds maximum %d", len(vs), iterations*writers)
				}
			}
		}()
	}

	wg.Wait()
}

func TestManager_SaveVersion_PropagatesMarshalErrors(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	bad := map[string]any{
		"chan": make(chan int),
	}
	v, err := vm.SaveVersion(bad, nil)
	assert.Nil(t, v)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version:")

	v, err = vm.SaveVersion(map[string]any{"valid": true}, map[string]any{"chan": make(chan int)})
	assert.Nil(t, v)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal metadata")
}

func TestManager_DefaultStorage_NoSourceTreeArtifacts(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(cwd) }()

	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	vm := NewVersionManager("", 0)
	v, err := vm.SaveVersion(map[string]any{"key": "value"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, v.VersionID)

	got := vm.GetVersion(v.VersionID)
	require.NotNil(t, got)
	assert.Equal(t, "value", got.Config["key"])

	_, statErr := os.Stat(filepath.Join(tmp, ".confii"))
	assert.True(t, os.IsNotExist(statErr),
		"default manager must not create .confii/ in the working tree")
}

func TestManager_TimestampOrdering_StrictlyMonotonic(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 100)

	const n = 50
	timestamps := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		v, err := vm.SaveVersion(map[string]any{"i": i}, nil)
		require.NoError(t, err)
		timestamps = append(timestamps, v.Timestamp)
	}

	for i := 1; i < len(timestamps); i++ {
		assert.True(t, timestamps[i].After(timestamps[i-1]),
			"timestamp at index %d (%v) is not strictly greater than previous (%v)",
			i, timestamps[i], timestamps[i-1])
	}
}

func TestManager_TimestampOrdering_AdvancesPastClockTie(t *testing.T) {
	vm := NewVersionManager("", 100)
	previous := time.Now().UTC().Add(time.Second)
	vm.lastTimestamp = previous

	v, err := vm.SaveVersion(map[string]any{"platform": "coarse-clock"}, nil)
	require.NoError(t, err)
	assert.True(t, v.Timestamp.After(previous))
}
