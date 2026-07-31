// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
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

func TestSet_NewKey_DocumentedDivergence(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("runtime.only.key", "v1"))

	info := cfg.Explain("runtime.only.key")
	require.Equal(t, true, info["exists"])
	assert.Equal(t, "runtime", info["source"],
		"Set must register runtime source via sourceTracker.TrackValue")

	var fired int64
	cfg.OnChange(func(key string, oldVal, newVal any) {
		atomic.AddInt64(&fired, 1)
	})

	require.NoError(t, cfg.ReloadWithContext(context.Background()))
	assert.Equal(t, int64(0), atomic.LoadInt64(&fired),
		"Reload over unchanged file must short-circuit at incremental gate; "+
			"runtime-Set keys must NOT distort the fileTracker contract")

	got, gErr := cfg.Get("runtime.only.key")
	require.NoError(t, gErr)
	assert.Equal(t, "v1", got)
}

func TestSet_NewKey_PostReload_RuntimeMutationPersists(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("runtime.only.A", "rt-A"))
	require.NoError(t, cfg.Set("runtime.only.B", 42))

	require.NoError(t, cfg.ReloadWithContext(context.Background()))

	gotA, errA := cfg.Get("runtime.only.A")
	require.NoError(t, errA)
	assert.Equal(t, "rt-A", gotA,
		"runtime key A must persist across no-op Reload (gate short-circuit)")

	gotB, errB := cfg.Get("runtime.only.B")
	require.NoError(t, errB)
	assert.Equal(t, 42, gotB,
		"runtime key B must persist across no-op Reload")

	infoA := cfg.Explain("runtime.only.A")
	assert.Equal(t, "runtime", infoA["source"])
	infoB := cfg.Explain("runtime.only.B")
	assert.Equal(t, "runtime", infoB["source"])
}

func TestSet_NewKey_PostReload_OnChange_FiresCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "g12-newkey.yaml")
	require.NoError(t, os.WriteFile(path, []byte("seed: v1\n"), 0600))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	type ev struct {
		key            string
		oldVal, newVal any
	}
	var (
		mu     sync.Mutex
		events []ev
	)
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if key != "runtime.only.A" {
			return
		}
		mu.Lock()
		events = append(events, ev{key, oldVal, newVal})
		mu.Unlock()
	})

	require.NoError(t, cfg.Set("runtime.only.A", "rt-A"))

	mu.Lock()
	require.Len(t, events, 1, "Set on a brand-new key must fire OnChange once")
	assert.Equal(t, "runtime.only.A", events[0].key)
	assert.Nil(t, events[0].oldVal,
		"new-key Set must emit oldVal=nil under union-iteration addition contract")
	assert.Equal(t, "rt-A", events[0].newVal)
	mu.Unlock()

	require.NoError(t, cfg.ReloadWithContext(context.Background()))

	mu.Lock()
	assert.Len(t, events, 1,
		"no-op Reload (gate short-circuit) must NOT fire OnChange for runtime key")
	mu.Unlock()

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("seed: v2\n"), 0600))
	require.NoError(t, cfg.ReloadWithContext(context.Background()))

	mu.Lock()
	require.Len(t, events, 2,
		"file-mutating Reload must fire eviction for runtime-only key")
	assert.Equal(t, "runtime.only.A", events[1].key)
	assert.Equal(t, "rt-A", events[1].oldVal,
		"Reload eviction emits prior runtime value as oldVal")
	assert.Nil(t, events[1].newVal,
		"Reload eviction emits newVal=nil under union-iteration deletion contract")
	mu.Unlock()

	_, getErr := cfg.Get("runtime.only.A")
	assert.Error(t, getErr,
		"runtime-only key must be evicted by file-mutating Reload (documented Reload semantics)")
}
