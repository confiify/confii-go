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

func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	return path
}

func TestConfig_Concurrent_SetAndGet(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

	for _, k := range keys {
		v, err := cfg.Get(k)
		require.NoError(t, err, "key %s missing after concurrent writes", k)
		require.NotNil(t, v)
	}
}

func TestConfig_Concurrent_OverlappingSetSameKey(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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
	require.IsType(t, "", v)
}

func TestConfig_Concurrent_ReloadAndSet(t *testing.T) {
	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 1\n")
	cfg, err := confii.NewWithContext[any](context.Background(),
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

			if err := cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)); err != nil {
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

func TestConfig_Concurrent_OverrideAndRestore(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_Concurrent_OnChangeCallbackDuringReload(t *testing.T) {
	path := writeTempYAML(t, "service:\n  name: alpha\n  count: 0\n")
	cfg, err := confii.NewWithContext[any](context.Background(),
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
			if err := cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)); err != nil {
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

	assert.Greater(t, fires.Load(), int64(0),
		"expected OnChange callback to fire during concurrent reloads")
}

func TestConfig_Concurrent_VersioningSaveAndRollback(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

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

func TestConfig_Concurrent_ObservabilityAndIntrospection(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_ToDict_ReturnsLiveMap_BlockedByG10(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	d, err := cfg.ToDict()
	require.NoError(t, err)
	require.NotNil(t, d)
	d["g36_probe_key"] = "leaked"

	d2, err := cfg.ToDict()
	require.NoError(t, err)
	assert.NotContains(t, d2, "g36_probe_key",
		"post- ToDict returns a defensive copy; mutation must not leak")
	assert.False(t, cfg.Has("g36_probe_key"),
		"post- ToDict mutation must not bleed into Has")
}

func TestConfig_ToDict_ConcurrentMutationByCallerIsUnsafe(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				d, err := cfg.ToDict()
				if err != nil {
					return
				}
				d["caller_mutation"] = "should_be_isolated"
			}
		}
	}()

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

	assert.False(t, cfg.Has("caller_mutation"),
		"caller mutation of ToDict result leaked into live config — defensive copy is broken")
}

func TestConfig_Concurrent_FreezeWhileSet(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

		close(freezeStarted)
		cfg.Freeze()
	}()

	<-freezeStarted
	wg.Wait()

	err = cfg.Set("post.freeze", 1)
	require.Error(t, err)
	assert.True(t, cfg.IsFrozen())
}
