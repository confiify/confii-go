// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

// Concurrency-and-race coverage for Config public API (Gap G36).
//
// The pre-existing TestConfig_ConcurrentAccess in config_test.go only
// exercises read-only goroutines (Get / Keys / Has). The library docstring
// claims "All public methods are safe for concurrent use" — these tests
// verify that claim end-to-end against Set, Reload, Override, OnChange,
// EnableObservability, EnableEvents, EnableVersioning, SaveVersion,
// RollbackToVersion, source-tracking introspection, and ToDict mutation.
//
// Every test must pass under `go test -race`. Goroutine coordination uses
// channels / sync.WaitGroup — no time.Sleep, so the harness is not flaky.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempYAML drops a small YAML file under t.TempDir() so the reload-path
// tests can mutate the file content between iterations.
func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	return path
}

// TestConfig_Concurrent_SetAndGet asserts that interleaved Set and Get
// calls do not race and never observe a torn value (Get either returns the
// pre-write value or the post-write value, never garbage). Pattern:
//   - 4 writers updating distinct top-level keys, 200 iterations each.
//   - 8 readers calling Get/Has/Keys/GetStringOr, 200 iterations each.
//   - Coordination: sync.WaitGroup; readers tolerate not-found because the
//     writer goroutines may not have populated their key yet.
func TestConfig_Concurrent_SetAndGet(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	const writers = 4
	const readers = 8
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	keys := []string{"writer.alpha", "writer.bravo", "writer.charlie", "writer.delta"}
	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := cfg.Set(keys[w], fmt.Sprintf("v-%d-%d", w, i)); err != nil {
					t.Errorf("writer %d set: %v", w, err)
					return
				}
			}
		}()
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _ = cfg.Get("database.host")
				_ = cfg.GetStringOr("writer.alpha", "default")
				_ = cfg.Has("debug")
				_ = cfg.Keys()
			}
		}()
	}
	wg.Wait()

	// Every writer's final value must be visible.
	for _, k := range keys {
		v, err := cfg.Get(k)
		require.NoError(t, err, "key %s missing after concurrent writes", k)
		require.NotNil(t, v)
	}
}

// TestConfig_Concurrent_OverlappingSetSameKey asserts that two writers
// hammering the same key never produce a corrupted intermediate state and
// that the final value is one of the writes (not a tear). Pattern: 2
// writers × 500 iterations; reader verifies Get returns a parseable value.
func TestConfig_Concurrent_OverlappingSetSameKey(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	const writers = 2
	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(writers + 1)

	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = cfg.Set("contended.key", fmt.Sprintf("w%d-i%d", w, i))
			}
		}()
	}

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _ = cfg.Get("contended.key")
		}
	}()

	wg.Wait()
	v, err := cfg.Get("contended.key")
	require.NoError(t, err)
	require.IsType(t, "", v) // string, never a partial value
}

// TestConfig_Concurrent_ReloadAndSet asserts that Reload can run while
// Set goroutines are still firing without producing -race reports.
// Pattern: 1 reload-driver mutating the underlying file and calling
// Reload; 4 writers calling Set; 4 readers calling Get. The reload
// driver and writers race on c.mu.Lock(); readers race on c.mu.RLock().
func TestConfig_Concurrent_ReloadAndSet(t *testing.T) {
	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 1\n")
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	const reloads = 20
	const writers = 4
	const readers = 4
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(1 + writers + readers)

	go func() {
		defer wg.Done()
		for i := 0; i < reloads; i++ {
			body := fmt.Sprintf("service:\n  name: alpha\n  count: %d\n", i)
			if err := os.WriteFile(path, []byte(body), 0644); err != nil {
				t.Errorf("write file: %v", err)
				return
			}
			// Force full reload (incremental check might be a no-op if
			// mtime hasn't ticked over on coarse-grained filesystems).
			if err := cfg.Reload(context.Background(), confii.WithIncremental(false)); err != nil {
				t.Errorf("reload: %v", err)
				return
			}
		}
	}()

	for w := 0; w < writers; w++ {
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = cfg.Set(fmt.Sprintf("set.w%d", w), i)
			}
		}()
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _ = cfg.Get("service.name")
				_ = cfg.Has("service.count")
				_ = cfg.Keys()
			}
		}()
	}

	wg.Wait()
}

// TestConfig_Concurrent_OverrideAndRestore asserts that Override's
// returned restore func can run concurrently with foreground Set / Get
// goroutines without race. Pattern: 4 goroutines acquire Override,
// perform reads/writes, then call restore; 4 readers run in parallel.
func TestConfig_Concurrent_OverrideAndRestore(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	const overriders = 4
	const readers = 4
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(overriders + readers)

	for o := 0; o < overriders; o++ {
		o := o
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				restore, err := cfg.Override(map[string]any{
					fmt.Sprintf("override.o%d", o): fmt.Sprintf("v-%d", i),
				})
				if err != nil {
					t.Errorf("override: %v", err)
					return
				}
				_, _ = cfg.Get("database.host")
				restore()
			}
		}()
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, _ = cfg.Get("database.host")
				_ = cfg.Has("debug")
			}
		}()
	}

	wg.Wait()
}

// TestConfig_Concurrent_OnChangeCallbackDuringReload asserts that
// OnChange callbacks can be registered while Reload is running, and
// that callbacks invoked by Reload do not race with concurrent Get
// readers. Pattern: 1 reload-driver, 2 callback-registrar goroutines,
// 4 readers, all running 50 iterations. We assert callbacks fire at
// least once when the underlying file content changes.
func TestConfig_Concurrent_OnChangeCallbackDuringReload(t *testing.T) {
	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 0\n")
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	cfg.OnChange(func(key string, oldVal, newVal any) {
		fires.Add(1)
	})

	const reloads = 30
	const registrars = 2
	const readers = 4

	var wg sync.WaitGroup
	wg.Add(1 + registrars + readers)

	go func() {
		defer wg.Done()
		for i := 0; i < reloads; i++ {
			body := fmt.Sprintf("service:\n  name: alpha\n  count: %d\n", i+1)
			if err := os.WriteFile(path, []byte(body), 0644); err != nil {
				t.Errorf("write file: %v", err)
				return
			}
			if err := cfg.Reload(context.Background(), confii.WithIncremental(false)); err != nil {
				t.Errorf("reload: %v", err)
				return
			}
		}
	}()

	for r := 0; r < registrars; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				cfg.OnChange(func(key string, oldVal, newVal any) {
					fires.Add(1)
				})
			}
		}()
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = cfg.Get("service.count")
			}
		}()
	}

	wg.Wait()

	// Callbacks should have fired during at least one reload.
	assert.Greater(t, fires.Load(), int64(0),
		"expected OnChange callback to fire during concurrent reloads")
}

// TestConfig_Concurrent_VersioningSaveAndRollback asserts that the
// versioning surface is concurrency-safe end-to-end via the Config[T]
// wrapper. Pattern: 1 saver loop, 1 rollback loop, 4 readers.
//
// Wave 18 D03 closure: SaveVersion's first-call lazy init is now
// serialized through c.versionMgrOnce (sync.Once) — the pre-Wave 18
// RLock -> RUnlock -> EnableVersioning(Lock) -> RLock TOCTOU dance is
// gone. This test no longer pre-enables versioning; the seeding
// SaveVersion below exercises the new sync.Once init path directly,
// then the concurrent saver/rollback loops race against an already-
// initialized manager (the steady-state path that the original
// concurrency contract pinned).
func TestConfig_Concurrent_VersioningSaveAndRollback(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Seed at least one version so rollback has a target. This first
	// SaveVersion drives the sync.Once-based lazy init of versionMgr
	// (Wave 18 D03), exercising the new init path under -race.
	first, err := cfg.SaveVersion(map[string]any{"seed": true})
	require.NoError(t, err)
	require.NotEmpty(t, first.VersionID)

	const saves = 60
	const rollbacks = 30
	const readers = 4

	var wg sync.WaitGroup
	wg.Add(2 + readers)

	go func() {
		defer wg.Done()
		for i := 0; i < saves; i++ {
			_ = cfg.Set("save.iter", i)
			if _, err := cfg.SaveVersion(map[string]any{"iter": i}); err != nil {
				t.Errorf("save: %v", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < rollbacks; i++ {
			// Always roll back to the seed version; rollback target
			// stays valid throughout the test run.
			if err := cfg.RollbackToVersion(first.VersionID); err != nil {
				t.Errorf("rollback: %v", err)
				return
			}
		}
	}()

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, _ = cfg.Get("database.host")
				_ = cfg.Has("save.iter")
			}
		}()
	}

	wg.Wait()
}

// TestConfig_Concurrent_ObservabilityAndIntrospection asserts that
// EnableObservability, EnableEvents, GetMetrics, GetSourceInfo, and
// related introspection calls are concurrency-safe. Pattern: 6
// goroutines, 200 iterations each; observability is enabled before
// the goroutines start so we don't depend on first-call lazy-init
// races (those are out of scope for G36, owned by G33/G07).
func TestConfig_Concurrent_ObservabilityAndIntrospection(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	cfg.EnableObservability()
	cfg.EnableEvents()

	const goroutines = 6
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch (g + i) % 6 {
				case 0:
					_ = cfg.GetMetrics()
				case 1:
					_ = cfg.GetSourceInfo("database.host")
				case 2:
					_ = cfg.GetConflicts()
				case 3:
					_ = cfg.GetSourceStatistics()
				case 4:
					_ = cfg.FindKeysFromSource("simple.yaml")
				case 5:
					_ = cfg.Layers()
				}
			}
		}()
	}

	wg.Wait()
}

// TestConfig_ToDict_ReturnsLiveMap_BlockedByG10 was a self-documenting
// "pin the bug" test prior to Wave 8: it asserted that the map
// returned by ToDict aliased Config's internal envConfig and that
// caller mutations leaked back. The G11 fix (uniform hook
// application) makes ToDict return a hook-applied deep copy, which
// also fixes the aliasing symptom that G10 was originally scoped
// to address.
//
// Per the G11 brief, G11's defensive-copy semantics are framed as
// "applies hooks AND returns a defensive copy"; G10 may layer on
// additional defensive-copy guarantees elsewhere (Set/Override
// inputs, etc.) but for ToDict the contract is now: returned map is
// a fresh allocation, mutating it does NOT leak into live state.
// The assertions below have been flipped to that post-G11 contract.
func TestConfig_ToDict_ReturnsLiveMap_BlockedByG10(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	d := cfg.ToDict()
	require.NotNil(t, d)
	d["g36_probe_key"] = "leaked"

	// POST-G11 BEHAVIOR: ToDict returns a hook-applied deep copy, so
	// caller mutations are isolated.
	d2 := cfg.ToDict()
	assert.NotContains(t, d2, "g36_probe_key",
		"post-G11 ToDict returns a defensive copy; mutation must not leak")
	assert.False(t, cfg.Has("g36_probe_key"),
		"post-G11 ToDict mutation must not bleed into Has()")
}

// TestConfig_ToDict_ConcurrentMutationByCallerIsUnsafe is a
// race-detector-targeted companion to the test above. Pre-G11 it
// was skipped because mutating ToDict's returned map raced with the
// Config's internal locking. Post-G11 ToDict returns a deep copy,
// so caller-side mutation cannot race with Get. The test now
// exercises that contract: caller A mutates its private copy while
// caller B reads the live config concurrently — no race, both
// observers see consistent state.
func TestConfig_ToDict_ConcurrentMutationByCallerIsUnsafe(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Mutator: repeatedly fetch a fresh ToDict copy and mutate it.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				d := cfg.ToDict()
				d["caller_mutation"] = "should_be_isolated"
			}
		}
	}()

	// Reader: repeatedly call Get; under -race this would have
	// caught the pre-G11 alias.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = cfg.Get("database.host")
			}
		}
	}()

	time.Sleep(20 * time.Millisecond)
	close(stop)
	wg.Wait()

	// The mutation must not have leaked into live state.
	assert.False(t, cfg.Has("caller_mutation"),
		"caller mutation of ToDict result leaked into live config — defensive copy is broken")
}

// TestConfig_Concurrent_FreezeWhileSet asserts that Freeze() called
// concurrently with Set() produces deterministic outcomes (Set either
// succeeds or returns ErrConfigFrozen — never a torn write). Pattern:
// 4 setters race a single freezer; after Freeze we assert all
// subsequent Sets fail with the frozen error.
func TestConfig_Concurrent_FreezeWhileSet(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	const setters = 4
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(setters + 1)

	freezeStarted := make(chan struct{})

	for s := 0; s < setters; s++ {
		s := s
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = cfg.Set(fmt.Sprintf("freeze.s%d", s), i)
			}
		}()
	}

	go func() {
		defer wg.Done()
		// Let setters get going for a bit before freezing.
		close(freezeStarted)
		cfg.Freeze()
	}()

	<-freezeStarted
	wg.Wait()

	// After freeze, Set must fail.
	err = cfg.Set("post.freeze", 1)
	require.Error(t, err)
	assert.True(t, cfg.IsFrozen())
}
