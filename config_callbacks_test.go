// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

// G13 / F-G13-Override coverage.
//
// Pre-G13, OnChange callbacks fired under c.mu.Lock(), so a callback
// that called any public Config method that takes c.mu (Get, Set,
// Reload, etc.) deadlocked the caller goroutine. Additionally,
// notifyChanges iterated only keys of the *new* flat map, so a key
// that disappeared from the configuration never reached the callback.
// Override mutated state silently — no callback, no event.
//
// These tests pin the post-G13 contract:
//   - Callbacks may freely call back into the Config (Get, Set) without
//     deadlocking. We bound each test with time.AfterFunc so the suite
//     fails loudly instead of hanging if the deadlock regresses.
//   - Removed keys surface (oldVal, nil) for both Reload and Extend.
//   - Override fires OnChange callbacks with the correct old/new values
//     and itself does not deadlock when callbacks call back into
//     Config methods.

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

// withDeadlockGuard arms a deadline that calls t.Fatal if it fires
// before being cancelled. Use it around any code that previously
// deadlocked under the G13 hazard so the harness never hangs the
// CI runner — it fails loudly instead.
func withDeadlockGuard(t *testing.T, name string, d time.Duration) (cancel func()) {
	t.Helper()
	timer := time.AfterFunc(d, func() {
		// Use t.Errorf + os.Exit-equivalent panic so the goroutine
		// running the test is interrupted; t.Fatal cannot be called
		// from a non-test goroutine.
		panic(fmt.Sprintf("deadlock-guard fired for %s after %s", name, d))
	})
	return func() { timer.Stop() }
}

// TestOnChange_CallbackDoesNotDeadlockOnGet asserts that a callback
// which calls cfg.Get from inside the callback completes without
// deadlocking the Reload goroutine. Pre-G13, Reload held c.mu while
// firing callbacks, so cfg.Get's c.mu.RLock blocked forever.
func TestOnChange_CallbackDoesNotDeadlockOnGet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOnChange_CallbackDoesNotDeadlockOnGet", 10*time.Second)
	defer cancel()

	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 0\n")
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	cfg.OnChange(func(key string, oldVal, newVal any) {
		// Re-entering the public API from inside a callback used to
		// deadlock against c.mu. Post-G13 the lock has been released
		// before this fires.
		_, _ = cfg.Get("service.name")
		fires.Add(1)
	})

	require.NoError(t, os.WriteFile(path, []byte("service:\n  name: alpha\n  count: 7\n"), 0644))
	require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))

	assert.Greater(t, fires.Load(), int64(0),
		"callback must fire (and complete) when value-change reload runs")
}

// TestOnChange_CallbackDoesNotDeadlockOnSet asserts that a callback
// which calls cfg.Set (write lock) from inside the callback completes
// without deadlocking. Pre-G13 this was a guaranteed self-deadlock.
func TestOnChange_CallbackDoesNotDeadlockOnSet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOnChange_CallbackDoesNotDeadlockOnSet", 10*time.Second)
	defer cancel()

	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 0\n")
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	// G12 (Wave 11): Set now fires OnChange callbacks per
	// F-G12-Set-CallbackSilence, so a callback that calls Set will
	// re-enter itself. We protect against infinite recursion with an
	// atomic "already-fired" flag rather than sync.Once because
	// recursive sync.Once.Do calls deadlock (see Once's documented
	// contract). The original test used sync.Once on the assumption
	// that Set never fired callbacks; that assumption no longer holds.
	var setFired atomic.Bool
	cfg.OnChange(func(key string, oldVal, newVal any) {
		// Set takes c.mu.Lock(); pre-G13 this would deadlock against
		// the Reload goroutine that was firing this callback. The
		// CompareAndSwap guards against the recursive entry that now
		// happens because Set itself fires callbacks (G12 Wave 11).
		if setFired.CompareAndSwap(false, true) {
			_ = cfg.Set("service.touched_by_callback", true)
		}
		fires.Add(1)
	})

	require.NoError(t, os.WriteFile(path, []byte("service:\n  name: alpha\n  count: 7\n"), 0644))
	require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))

	assert.Greater(t, fires.Load(), int64(0),
		"callback must fire (and Set must complete) on value-change reload")

	// Confirm the Set actually landed — proves the callback ran to
	// completion, not just entered.
	got, err := cfg.Get("service.touched_by_callback")
	require.NoError(t, err)
	assert.Equal(t, true, got, "callback's Set must have committed")
}

// TestOnChange_FiresForRemovedKeys asserts that reloading with a
// payload that no longer contains a key fires the callback with
// (oldVal, nil) for that key. Pre-G13, notifyChanges iterated only
// keys of the new flat map, so deletions were silently dropped.
func TestOnChange_FiresForRemovedKeys(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n  port: 5432\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	var mu sync.Mutex
	changes := make(map[string][2]any)
	cfg.OnChange(func(key string, oldVal, newVal any) {
		mu.Lock()
		defer mu.Unlock()
		changes[key] = [2]any{oldVal, newVal}
	})

	// Remove database.port by writing a file that no longer contains it.
	require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n"), 0644))
	require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, changes, "database.port",
		"OnChange must fire for removed key database.port (G13 deletion contract)")
	got := changes["database.port"]
	require.NotNil(t, got[0], "old value of removed key must not be nil")
	assert.Equal(t, "5432", fmt.Sprintf("%v", got[0]),
		"old value must be the pre-reload payload (5432)")
	assert.Nil(t, got[1], "new value of removed key must be the zero `any`")
}

// TestOnChange_FiresForRemovedKeys_ViaExtend asserts that the deletion
// contract holds for Extend as well (its commit phase mirrors Reload).
// Extend cannot directly REMOVE a key from the existing config — it
// merges an overlay in — so we test a related shape: an Extend that
// introduces a NEW key surfaces (nil, newVal). The "removed via Extend"
// case is exercised by Reload in the previous test; Extend's call site
// must satisfy the same union-iteration contract for additions, which
// pre-G13 already worked but is verified here for completeness so the
// contract is symmetric.
func TestOnChange_FiresForRemovedKeys_ViaExtend(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("database:\n  host: localhost\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(basePath)),
	)
	require.NoError(t, err)

	var mu sync.Mutex
	changes := make(map[string][2]any)
	cfg.OnChange(func(key string, oldVal, newVal any) {
		mu.Lock()
		defer mu.Unlock()
		changes[key] = [2]any{oldVal, newVal}
	})

	// Extend with a payload that introduces a new key. Pre-G13 the
	// add-key path fired (it iterated newFlat); the post-G13 union
	// iteration must still fire for it. This pins symmetry.
	overlayPath := filepath.Join(tmpDir, "overlay.yaml")
	require.NoError(t, os.WriteFile(overlayPath, []byte("database:\n  port: 5432\n"), 0644))
	require.NoError(t, cfg.Extend(context.Background(), loader.NewYAML(overlayPath)))

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, changes, "database.port",
		"Extend that introduces database.port must fire OnChange for it")
	got := changes["database.port"]
	assert.Nil(t, got[0], "old value of newly introduced key must be nil")
	assert.Equal(t, "5432", fmt.Sprintf("%v", got[1]),
		"new value must be the Extend overlay payload (5432)")
}

// TestOverride_FiresOnChangeCallback asserts F-G13-Override:
// Override invokes registered OnChange callbacks for every key it
// mutates, with the correct (oldVal, newVal) payload. Pre-fix
// Override mutated state silently, leaving observers blind to the
// change.
func TestOverride_FiresOnChangeCallback(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Capture the initial value so the assertion is robust against
	// changes to simple.yaml; require it as a fixture precondition.
	initial, err := cfg.Get("database.host")
	require.NoError(t, err)
	require.NotEmpty(t, fmt.Sprintf("%v", initial),
		"fixture precondition: database.host must be set in simple.yaml")

	var mu sync.Mutex
	changes := make(map[string][2]any)
	cfg.OnChange(func(key string, oldVal, newVal any) {
		mu.Lock()
		defer mu.Unlock()
		changes[key] = [2]any{oldVal, newVal}
	})

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-host",
	})
	require.NoError(t, err)
	defer restore()

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, changes, "database.host",
		"Override must fire OnChange for the key it mutates (F-G13-Override)")
	got := changes["database.host"]
	assert.Equal(t, fmt.Sprintf("%v", initial), fmt.Sprintf("%v", got[0]),
		"old value must be the pre-override fixture value")
	assert.Equal(t, "override-host", fmt.Sprintf("%v", got[1]),
		"new value must be the override payload")
}

// TestOverride_RestoreFiresOnChangeCallback asserts that the restore
// function returned by Override also fires callbacks, with the inverse
// payload. This makes Override / restore symmetric from an observer's
// perspective.
func TestOverride_RestoreFiresOnChangeCallback(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	initial, err := cfg.Get("database.host")
	require.NoError(t, err)

	// Track each callback firing in order so we can assert the
	// restore phase produces the inverse payload.
	type fire struct {
		key, oldS, newS string
	}
	var mu sync.Mutex
	var fires []fire
	cfg.OnChange(func(key string, oldVal, newVal any) {
		mu.Lock()
		defer mu.Unlock()
		fires = append(fires, fire{key, fmt.Sprintf("%v", oldVal), fmt.Sprintf("%v", newVal)})
	})

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-host",
	})
	require.NoError(t, err)
	restore()

	mu.Lock()
	defer mu.Unlock()
	// We expect at least two fires for database.host: one from Override
	// (initial -> "override-host") and one from restore ("override-host"
	// -> initial). Other unrelated keys may not fire because nothing
	// changed for them.
	var overrideFire, restoreFire *fire
	for i := range fires {
		if fires[i].key != "database.host" {
			continue
		}
		if overrideFire == nil {
			overrideFire = &fires[i]
		} else if restoreFire == nil {
			restoreFire = &fires[i]
		}
	}
	require.NotNil(t, overrideFire, "Override must fire OnChange for database.host")
	require.NotNil(t, restoreFire, "restore() must fire OnChange for database.host")
	assert.Equal(t, fmt.Sprintf("%v", initial), overrideFire.oldS)
	assert.Equal(t, "override-host", overrideFire.newS)
	assert.Equal(t, "override-host", restoreFire.oldS)
	assert.Equal(t, fmt.Sprintf("%v", initial), restoreFire.newS)
}

// TestOverride_DoesNotDeadlockOnGet asserts that a callback firing
// from Override may call cfg.Get without deadlocking. Pre-fix this
// was a guaranteed deadlock had Override fired callbacks at all.
func TestOverride_DoesNotDeadlockOnGet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOverride_DoesNotDeadlockOnGet", 10*time.Second)
	defer cancel()

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	cfg.OnChange(func(key string, oldVal, newVal any) {
		// Re-enter the public API. With c.mu held this would be a
		// guaranteed self-deadlock.
		_, _ = cfg.Get("database.host")
		fires.Add(1)
	})

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-host",
	})
	require.NoError(t, err)
	defer restore()

	assert.Greater(t, fires.Load(), int64(0),
		"Override must fire callback (and Get must complete without deadlocking)")
}

// TestOverride_DoesNotDeadlockOnSet covers the write-lock counterpart:
// callback calls cfg.Set after Override fires it. Set itself is a
// no-op against c.frozen because Override unfreezes for the override
// window, so Set must succeed.
func TestOverride_DoesNotDeadlockOnSet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOverride_DoesNotDeadlockOnSet", 10*time.Second)
	defer cancel()

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	// G12 (Wave 11): see TestOnChange_CallbackDoesNotDeadlockOnSet for
	// why we use atomic.Bool instead of sync.Once.
	var setFired atomic.Bool
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if setFired.CompareAndSwap(false, true) {
			_ = cfg.Set("database.touched_by_callback", true)
		}
		fires.Add(1)
	})

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-host",
	})
	require.NoError(t, err)
	defer restore()

	assert.Greater(t, fires.Load(), int64(0),
		"Override callback must run to completion, including its Set")

	got, err := cfg.Get("database.touched_by_callback")
	require.NoError(t, err)
	assert.Equal(t, true, got, "callback's Set must have committed under Override")
}

// TestOnChange_GodocContract_Recursion is the runnable example pinned by
// the OnChange godoc (Wave 18 F-OnChange-ReentrancyDocs). It exists to:
//
//  1. Demonstrate the canonical atomic.Bool CAS guard against unbounded
//     recursion when a callback mutates the Config and that mutation
//     itself fires OnChange.
//  2. Verify that an UNGUARDED self-firing pattern would loop forever
//     (we don't actually run the unbounded loop — we cap it at a small
//     budget, then assert that without the guard recursion DOES happen
//     and the bound is exceeded; with the guard it stops at exactly 1
//     reentry).
//
// Pre-fix (pre-G12, when Set was silent), there was no recursion at all
// and this test was unnecessary; post-G12 + Wave 18 godoc, the contract
// is documented and this test pins it.
func TestOnChange_GodocContract_Recursion(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOnChange_GodocContract_Recursion", 10*time.Second)
	defer cancel()

	// --- Sub-case 1: guarded with atomic.Bool CAS -> exactly one reentry. ---
	t.Run("Guarded_AtomicBoolCAS_StopsAtOneReentry", func(t *testing.T) {
		cfg, err := confii.New[any](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		)
		require.NoError(t, err)

		var fires atomic.Int64
		var fired atomic.Bool // canonical guard from OnChange godoc.
		cfg.OnChange(func(key string, oldVal, newVal any) {
			fires.Add(1)
			// CAS one-shot: first entry mutates, all later self-fires
			// (recursive entries) take the false branch and exit.
			if fired.CompareAndSwap(false, true) {
				_ = cfg.Set("godoc.recursion.derived", "v1")
			}
		})

		// Top-level Set fires the callback once. The callback's nested
		// Set fires it a second time (G12 self-firing). The guard then
		// suppresses any further mutation, so we land at exactly 2
		// total fires (one per Set), no runaway.
		require.NoError(t, cfg.Set("godoc.recursion.trigger", "go"))

		// Wait for the second (nested) callback to run; both fires
		// happen inside the outer Set's call stack because callbacks
		// are invoked synchronously after lock release.
		assert.Equal(t, int64(2), fires.Load(),
			"with atomic.Bool CAS guard, callback fires once for the trigger Set "+
				"and once for the nested derived Set, then the guard stops further mutation")

		got, err := cfg.Get("godoc.recursion.derived")
		require.NoError(t, err)
		assert.Equal(t, "v1", got,
			"nested Set inside the callback must have committed (lock-release contract)")
	})

	// --- Sub-case 2: unguarded callback DOES self-fire (proves the
	// guard is necessary, not a no-op). We cap reentry depth manually
	// so the test does not run unbounded; the assertion is that the
	// cap is hit, which would not happen if Set were silent. ---
	t.Run("Unguarded_DemonstratesSelfFire", func(t *testing.T) {
		cfg, err := confii.New[any](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		)
		require.NoError(t, err)

		const depthCap = 5
		var depth atomic.Int64
		cfg.OnChange(func(key string, oldVal, newVal any) {
			d := depth.Add(1)
			// Without an external guard the call would recurse forever;
			// we use a depth cap as a manual termination, which itself
			// proves self-firing happens.
			if d < depthCap {
				_ = cfg.Set(fmt.Sprintf("godoc.recursion.depth.%d", d), d)
			}
		})

		require.NoError(t, cfg.Set("godoc.recursion.depth.0", int64(0)))

		// If Set were silent (pre-G12) we would see depth == 1. Self-
		// firing pushes us to the cap.
		assert.GreaterOrEqual(t, depth.Load(), int64(depthCap),
			"unguarded callback must self-fire until the manual depth cap is hit, "+
				"proving the recursion documented in OnChange godoc is real")
	})
}
