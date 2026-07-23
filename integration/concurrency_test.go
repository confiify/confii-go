package integration

// End-to-end concurrency tests (Gap G36).
//
// The pre-existing TestConcurrentAccess in integration_test.go exercises
// only read-only goroutines (Get / Has / Keys / ToDict / GetStringOr).
// This file extends that coverage with full read/write/reload/override
// pairings against the same testdata used by the existing integration
// suite. All tests must pass under `go test -race`.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_Concurrent_SetReloadOverrideCallbacks runs a realistic
// end-to-end concurrency stress test against the integration testdata
// fixture. Pattern, all racing on a single Config[any]:
//   - 1 reload driver mutating the underlying YAML and calling Reload.
//   - 4 setters writing distinct keys.
//   - 2 override+restore goroutines.
//   - 4 readers (Get/Has/Keys/ToDict/Layers).
//   - 1 OnChange registrar adding callbacks during the run.
//
// The test asserts that no goroutine errors and that the OnChange
// callback fired at least once.
func TestIntegration_Concurrent_SetReloadOverrideCallbacks(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "base.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"default:\n  app:\n    name: alpha\n  database:\n    host: localhost\n    port: 5432\n",
	), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
		confii.WithEnv("default"),
	)
	require.NoError(t, err)

	var fires atomic.Int64
	cfg.OnChange(func(key string, oldVal, newVal any) {
		fires.Add(1)
	})

	const reloads = 20
	const setters = 4
	const overriders = 2
	const readers = 4
	const setIters = 100
	const overrideIters = 30
	const readIters = 100

	var wg sync.WaitGroup
	wg.Add(1 + setters + overriders + readers + 1)

	go func() {
		defer wg.Done()
		for i := 0; i < reloads; i++ {
			body := fmt.Sprintf(
				"default:\n  app:\n    name: alpha\n  database:\n    host: localhost\n    port: %d\n",
				5400+i,
			)
			if err := os.WriteFile(cfgPath, []byte(body), 0644); err != nil {
				t.Errorf("write: %v", err)
				return
			}
			if err := cfg.Reload(context.Background(), confii.WithIncremental(false)); err != nil {
				t.Errorf("reload: %v", err)
				return
			}
		}
	}()

	for s := 0; s < setters; s++ {
		s := s
		go func() {
			defer wg.Done()
			for i := 0; i < setIters; i++ {
				if err := cfg.Set(fmt.Sprintf("set.s%d", s), i); err != nil {
					t.Errorf("set: %v", err)
					return
				}
			}
		}()
	}

	for o := 0; o < overriders; o++ {
		o := o
		go func() {
			defer wg.Done()
			for i := 0; i < overrideIters; i++ {
				restore, err := cfg.Override(map[string]any{
					fmt.Sprintf("ov.o%d", o): fmt.Sprintf("v-%d", i),
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
			for i := 0; i < readIters; i++ {
				_, _ = cfg.Get("database.host")
				_ = cfg.Has("database.port")
				_ = cfg.Keys()
				_ = cfg.Layers()
				// ToDict returns a live map (G10 territory). We do
				// NOT mutate it here — that path is asserted in
				// TestConfig_ToDict_ReturnsLiveMap_BlockedByG10.
				_ = cfg.ToDict()
			}
		}()
	}

	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			cfg.OnChange(func(key string, oldVal, newVal any) {
				fires.Add(1)
			})
		}
	}()

	wg.Wait()

	assert.Greater(t, fires.Load(), int64(0),
		"expected OnChange callback to fire during the concurrent reload sequence")
}

// TestIntegration_Concurrent_SourceTrackingDuringExtend asserts that
// source-tracking and layer enumeration remain consistent when an
// Extend call adds a new loader concurrently with reads against the
// same Config. Pattern: 1 extender adding 5 in-memory loaders;
// 4 readers calling Layers / GetSourceInfo / FindKeysFromSource.
func TestIntegration_Concurrent_SourceTrackingDuringExtend(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "base.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"default:\n  app:\n    name: alpha\n",
	), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
		confii.WithEnv("default"),
	)
	require.NoError(t, err)

	const readers = 4
	const extends = 5
	const readIters = 200

	var wg sync.WaitGroup
	wg.Add(readers + 1)

	go func() {
		defer wg.Done()
		for i := 0; i < extends; i++ {
			extPath := filepath.Join(dir, fmt.Sprintf("ext_%d.yaml", i))
			if err := os.WriteFile(extPath, []byte(
				fmt.Sprintf("ext_key_%d: value_%d\n", i, i),
			), 0644); err != nil {
				t.Errorf("write ext: %v", err)
				return
			}
			if err := cfg.Extend(context.Background(), loader.NewYAML(extPath)); err != nil {
				t.Errorf("extend: %v", err)
				return
			}
		}
	}()

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < readIters; i++ {
				_ = cfg.Layers()
				_ = cfg.GetSourceInfo("app.name")
				_ = cfg.FindKeysFromSource("base.yaml")
			}
		}()
	}

	wg.Wait()

	// All extended keys must be visible after the run.
	for i := 0; i < extends; i++ {
		key := fmt.Sprintf("ext_key_%d", i)
		assert.True(t, cfg.Has(key), "extended key %s missing", key)
	}
}
