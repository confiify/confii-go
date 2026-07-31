// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionManager_ConcurrentSaveListGetDiff(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 500)

	seedA, err := vm.SaveVersion(map[string]any{"seed": "A", "n": 1}, nil)
	require.NoError(t, err)
	seedB, err := vm.SaveVersion(map[string]any{"seed": "B", "n": 2}, nil)
	require.NoError(t, err)

	const savers = 3
	const listers = 3
	const latest = 3
	const differs = 2
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(savers + listers + latest + differs)

	for s := 0; s < savers; s++ {
		s := s
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if _, err := vm.SaveVersion(map[string]any{"saver": s, "iter": i},
					map[string]any{"who": fmt.Sprintf("s%d", s)},
				); err != nil {
					t.Errorf("saver %d: %v", s, err)
					return
				}
			}
		}()
	}

	for l := 0; l < listers; l++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				vs := vm.ListVersions()
				if len(vs) == 0 {
					t.Errorf("ListVersions empty")
					return
				}
			}
		}()
	}

	for l := 0; l < latest; l++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				v := vm.LatestVersion()
				if v == nil {
					t.Errorf("LatestVersion nil")
					return
				}
			}
		}()
	}

	for d := 0; d < differs; d++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				diffs, err := vm.DiffVersions(seedA.VersionID, seedB.VersionID)
				if err != nil {
					t.Errorf("diff: %v", err)
					return
				}
				_ = diffs
			}
		}()
	}

	wg.Wait()
}

func TestVersionManager_ConcurrentSaveAndGetVersion(t *testing.T) {
	dir := t.TempDir()
	vm := NewVersionManager(dir, 500)

	seed, err := vm.SaveVersion(map[string]any{"seed": true}, nil)
	require.NoError(t, err)

	const savers = 2
	const readers = 4
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(savers + readers)

	idCh := make(chan string, savers*iterations)

	for s := 0; s < savers; s++ {
		s := s
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				v, err := vm.SaveVersion(map[string]any{"s": s, "i": i}, nil)
				if err != nil {
					t.Errorf("save: %v", err)
					return
				}
				idCh <- v.VersionID
			}
		}()
	}

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				v := vm.GetVersion(seed.VersionID)
				if v == nil {
					t.Errorf("seed missing under concurrent saves")
					return
				}
				select {
				case id := <-idCh:
					_ = vm.GetVersion(id)
				default:
				}
			}
		}()
	}

	wg.Wait()
}

func TestVersionManager_EvictionUnderConcurrentSave(t *testing.T) {
	dir := t.TempDir()
	const maxVersions = 10
	vm := NewVersionManager(dir, maxVersions)

	const savers = 6
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(savers)
	var saved atomic.Int64

	for s := 0; s < savers; s++ {
		s := s
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if _, err := vm.SaveVersion(map[string]any{"s": s, "i": i},
					nil,
				); err != nil {
					t.Errorf("save: %v", err)
					return
				}
				saved.Add(1)
			}
		}()
	}
	wg.Wait()

	versions := vm.ListVersions()
	assert.LessOrEqual(t, len(versions), maxVersions,
		"eviction must keep version count within maxVersions")
	assert.Equal(t, int64(savers*iterations), saved.Load())
}
