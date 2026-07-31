// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withDeadlockGuard(t *testing.T, name string, d time.Duration) (cancel func()) {
	t.Helper()
	timer := time.AfterFunc(d, func() {

		panic(fmt.Sprintf("deadlock-guard fired for %s after %s", name, d))
	})
	return func() { timer.Stop() }
}

func TestOnChange_CallbackDoesNotDeadlockOnGet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOnChange_CallbackDoesNotDeadlockOnGet", 10*time.Second)
	defer cancel()

	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 0\n")
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	cfg.OnChange(func(key string, oldVal, newVal any) {
		_, _ = cfg.Get("service.name")
		fires.Add(1)
	})

	require.NoError(t, os.WriteFile(path, []byte("service:\n  name: alpha\n  count: 7\n"), 0644))
	require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))

	assert.Greater(t, fires.Load(), int64(0),
		"callback must fire (and complete) when value-change reload runs")
}

func TestOnChange_CallbackDoesNotDeadlockOnSet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOnChange_CallbackDoesNotDeadlockOnSet", 10*time.Second)
	defer cancel()

	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 0\n")
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	var setFired atomic.Bool
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if setFired.CompareAndSwap(false, true) {
			_ = cfg.Set("service.touched_by_callback", true)
		}
		fires.Add(1)
	})

	require.NoError(t, os.WriteFile(path, []byte("service:\n  name: alpha\n  count: 7\n"), 0644))
	require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))

	assert.Greater(t, fires.Load(), int64(0),
		"callback must fire (and Set must complete) on value-change reload")

	got, err := cfg.Get("service.touched_by_callback")
	require.NoError(t, err)
	assert.Equal(t, true, got, "callback's Set must have committed")
}

func TestOnChange_FiresForRemovedKeys(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n  port: 5432\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
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

	require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n"), 0644))
	require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, changes, "database.port",
		"OnChange must fire for removed key database.port (deletion contract)")
	got := changes["database.port"]
	require.NotNil(t, got[0], "old value of removed key must not be nil")
	assert.Equal(t, "5432", fmt.Sprintf("%v", got[0]),
		"old value must be the pre-reload payload (5432)")
	assert.Nil(t, got[1], "new value of removed key must be the zero `any`")
}

func TestOnChange_FiresForRemovedKeys_ViaExtend(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("database:\n  host: localhost\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
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

	overlayPath := filepath.Join(tmpDir, "overlay.yaml")
	require.NoError(t, os.WriteFile(overlayPath, []byte("database:\n  port: 5432\n"), 0644))
	require.NoError(t, cfg.ExtendWithContext(context.Background(), loader.NewYAML(overlayPath)))

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, changes, "database.port",
		"Extend that introduces database.port must fire OnChange for it")
	got := changes["database.port"]
	assert.Nil(t, got[0], "old value of newly introduced key must be nil")
	assert.Equal(t, "5432", fmt.Sprintf("%v", got[1]),
		"new value must be the Extend overlay payload (5432)")
}

func TestOverride_FiresOnChangeCallback(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

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
		"Override must fire OnChange for the key it mutates ")
	got := changes["database.host"]
	assert.Equal(t, fmt.Sprintf("%v", initial), fmt.Sprintf("%v", got[0]),
		"old value must be the pre-override fixture value")
	assert.Equal(t, "override-host", fmt.Sprintf("%v", got[1]),
		"new value must be the override payload")
}

func TestOverride_RestoreFiresOnChangeCallback(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	initial, err := cfg.Get("database.host")
	require.NoError(t, err)

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
	require.NotNil(t, restoreFire, "restore must fire OnChange for database.host")
	assert.Equal(t, fmt.Sprintf("%v", initial), overrideFire.oldS)
	assert.Equal(t, "override-host", overrideFire.newS)
	assert.Equal(t, "override-host", restoreFire.oldS)
	assert.Equal(t, fmt.Sprintf("%v", initial), restoreFire.newS)
}

func TestOverride_DoesNotDeadlockOnGet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOverride_DoesNotDeadlockOnGet", 10*time.Second)
	defer cancel()

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	cfg.OnChange(func(key string, oldVal, newVal any) {

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

func TestOverride_DoesNotDeadlockOnSet(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOverride_DoesNotDeadlockOnSet", 10*time.Second)
	defer cancel()

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var fires atomic.Int64
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

func TestOnChange_GodocContract_Recursion(t *testing.T) {
	cancel := withDeadlockGuard(t, "TestOnChange_GodocContract_Recursion", 10*time.Second)
	defer cancel()

	t.Run("Guarded_AtomicBoolCAS_StopsAtOneReentry", func(t *testing.T) {
		cfg, err := confii.NewWithContext[any](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		)
		require.NoError(t, err)

		var fires atomic.Int64
		var fired atomic.Bool
		cfg.OnChange(func(key string, oldVal, newVal any) {
			fires.Add(1)

			if fired.CompareAndSwap(false, true) {
				_ = cfg.Set("godoc.recursion.derived", "v1")
			}
		})

		require.NoError(t, cfg.Set("godoc.recursion.trigger", "go"))

		assert.Equal(t, int64(2), fires.Load(),
			"with atomic.Bool CAS guard, callback fires once for the trigger Set "+
				"and once for the nested derived Set(), then the guard stops further mutation")

		got, err := cfg.Get("godoc.recursion.derived")
		require.NoError(t, err)
		assert.Equal(t, "v1", got,
			"nested Set inside the callback must have committed (lock-release contract)")
	})

	t.Run("Unguarded_DemonstratesSelfFire", func(t *testing.T) {
		cfg, err := confii.NewWithContext[any](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		)
		require.NoError(t, err)

		const depthCap = 5
		var depth atomic.Int64
		cfg.OnChange(func(key string, oldVal, newVal any) {
			d := depth.Add(1)

			if d < depthCap {
				_ = cfg.Set(fmt.Sprintf("godoc.recursion.depth.%d", d), d)
			}
		})

		require.NoError(t, cfg.Set("godoc.recursion.depth.0", int64(0)))

		assert.GreaterOrEqual(t, depth.Load(), int64(depthCap),
			"unguarded callback must self-fire until the manual depth cap is hit, "+
				"proving the recursion documented in OnChange godoc is real")
	})
}
