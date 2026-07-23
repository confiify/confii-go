package observe

// Concurrency tests for observe.VersionManager beyond the G22 baseline
// (Gap G36).
//
// G22 added TestManager_ListVersions_NoMutationUnderConcurrentRead which
// covers SaveVersion × ListVersions. This file extends coverage to the
// remaining method pairings called out by G36:
//   - SaveVersion × GetVersion (read-after-write).
//   - SaveVersion × DiffVersions (cross-version comparison).
//   - SaveVersion × LatestVersion.
// All tests pass under `go test -race`.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVersionManager_ConcurrentSaveListGetDiff exercises every public
// surface of VersionManager from independent goroutines and asserts no
// data race. Pattern:
//   - 3 savers (200 iterations each).
//   - 3 listers (200 iterations each).
//   - 3 latest readers (200 iterations each).
//   - 2 diff goroutines that pick two known seed IDs and compare them.
//
// We seed two well-known versions before launching goroutines so the
// diff goroutines always have valid IDs.
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
				if _, err := vm.SaveVersion(
					map[string]any{"saver": s, "iter": i},
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

// TestVersionManager_ConcurrentSaveAndGetVersion asserts that
// GetVersion (which reads under RLock and falls through to disk) is
// safe to interleave with SaveVersion (which holds the write lock and
// writes to disk). Pattern:
//   - 2 savers, 200 iterations each, using sequential payloads.
//   - 4 readers querying GetVersion(known_id) repeatedly.
//
// We capture the IDs returned by the savers via a buffered channel
// and the readers consume them without blocking writers.
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

	// Reader goroutines: always retrieve the seed (which is guaranteed
	// to exist), and additionally drain produced IDs as they appear.
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
					// nothing yet
				}
			}
		}()
	}

	wg.Wait()
}

// TestVersionManager_EvictionUnderConcurrentSave asserts that the
// evict() path is race-free even when many goroutines are saving
// past the maxVersions boundary. Pattern: maxVersions=10, 6 savers,
// 100 iterations each. After all savers complete, len(versions) must
// not exceed maxVersions.
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
				if _, err := vm.SaveVersion(
					map[string]any{"s": s, "i": i},
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
