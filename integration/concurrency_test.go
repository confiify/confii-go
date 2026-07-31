// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_Concurrent_SetReloadOverrideCallbacks(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "base.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("default:\n  app:\n    name: alpha\n  database:\n    host: localhost\n    port: 5432\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
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
			body := fmt.Sprintf("default:\n  app:\n    name: alpha\n  database:\n    host: localhost\n    port: %d\n",
				5400+i,
			)
			if err := os.WriteFile(cfgPath, []byte(body), 0644); err != nil {
				t.Errorf("write: %v", err)
				return
			}
			if err := cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)); err != nil {
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
				_, _ = cfg.ToDict()
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

func TestIntegration_Concurrent_SourceTrackingDuringExtend(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "base.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("default:\n  app:\n    name: alpha\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
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
			if err := os.WriteFile(extPath, []byte(fmt.Sprintf("ext_key_%d: value_%d\n", i, i)), 0644); err != nil {
				t.Errorf("write ext: %v", err)
				return
			}
			if err := cfg.ExtendWithContext(context.Background(), loader.NewYAML(extPath)); err != nil {
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

	for i := 0; i < extends; i++ {
		key := fmt.Sprintf("ext_key_%d", i)
		assert.True(t, cfg.Has(key), "extended key %s missing", key)
	}
}
