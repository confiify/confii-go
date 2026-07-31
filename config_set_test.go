// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSet_SourceTrackingParity(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	pre := cfg.Explain("database.host")
	require.Equal(t, true, pre["exists"])
	require.NotEqual(t, "runtime", pre["source"],
		"fixture precondition: pre-Set source must be the file loader")

	require.NoError(t, cfg.Set("database.host", "set-by-runtime"))

	post := cfg.Explain("database.host")
	require.Equal(t, true, post["exists"])
	assert.Equal(t, "runtime", post["source"],
		"Explain must report source=runtime after Set (source-tracking parity)")
	assert.Equal(t, "runtime", post["loader_type"],
		"Explain must report loader_type=runtime after Set (source-tracking parity)")
	assert.Equal(t, "set-by-runtime", post["current_value"])
}

func TestSet_SourceTrackingParity_NewKey(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("runtime.added.key", 42))

	got := cfg.Explain("runtime.added.key")
	require.Equal(t, true, got["exists"])
	assert.Equal(t, "runtime", got["source"])
	assert.Equal(t, 42, got["current_value"])
}

func TestSet_PathErrorPropagatesAsTypedError(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	err = cfg.Set("debug.feature.flag", true)
	require.Error(t, err, "Set must surface the SetNested PathError ")

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		"expected *ConfigError, got %T (%v)", err, err)
	assert.Equal(t, "Set", ce.Op,
		"ConfigError.Op must be Set (typed-error contract)")
	assert.Equal(t, "debug.feature.flag", ce.Key,
		"ConfigError.Key must echo the rejected key path")
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"error must wrap ErrConfigInvalid sentinel ")

	dbg, gerr := cfg.GetBool("debug")
	require.NoError(t, gerr,
		"after a rejected Set(), the pre-Set debug value must still be queryable")
	assert.Equal(t, true, dbg)
}

func TestOverride_PathErrorPropagatesAsTypedError(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	restore, err := cfg.Override(map[string]any{
		"debug.feature": "x",
	})
	require.Error(t, err,
		"Override must surface SetNested PathError ")
	assert.Nil(t, restore,
		"Override must return nil restore on failure")

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, "Override", ce.Op)
	assert.Equal(t, "debug.feature", ce.Key)
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"Override error must wrap ErrConfigInvalid sentinel")

	dbg, gerr := cfg.GetBool("debug")
	require.NoError(t, gerr)
	assert.Equal(t, true, dbg)
}

func TestSet_FiresOnChangeCallback_AddAndUpdate(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	type fire struct {
		key, oldS, newS string
		oldNil, newNil  bool
	}
	var mu sync.Mutex
	var fires []fire
	cfg.OnChange(func(key string, oldVal, newVal any) {
		mu.Lock()
		defer mu.Unlock()
		fires = append(fires, fire{
			key:    key,
			oldS:   fmt.Sprintf("%v", oldVal),
			newS:   fmt.Sprintf("%v", newVal),
			oldNil: oldVal == nil,
			newNil: newVal == nil,
		})
	})

	require.NoError(t, cfg.Set("database.host", "set-by-test"))

	require.NoError(t, cfg.Set("freshly.added.key", "added"))

	mu.Lock()
	defer mu.Unlock()

	var update, addition *fire
	for i := range fires {
		switch fires[i].key {
		case "database.host":
			update = &fires[i]
		case "freshly.added.key":
			addition = &fires[i]
		}
	}
	require.NotNil(t, update,
		"Set on existing key must fire OnChange ")
	assert.False(t, update.oldNil,
		"update fire must report non-nil oldVal")
	assert.False(t, update.newNil,
		"update fire must report non-nil newVal")
	assert.Equal(t, "localhost", update.oldS)
	assert.Equal(t, "set-by-test", update.newS)

	require.NotNil(t, addition,
		"Set on new key must fire OnChange ")
	assert.True(t, addition.oldNil,
		"addition fire must report nil oldVal (deletion contract symmetry)")
	assert.False(t, addition.newNil)
	assert.Equal(t, "added", addition.newS)
}

func TestSet_DeepCopiesMapValue(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	original := map[string]any{"a": 1}
	require.NoError(t, cfg.Set("user.payload", original))

	original["a"] = 999
	original["b"] = "leaked"

	got, err := cfg.Get("user.payload.a")
	require.NoError(t, err)
	assert.Equal(t, 1, got,
		"Set must defensively deep-copy map values ")

	_, err = cfg.Get("user.payload.b")
	assert.Error(t, err,
		"caller-side keys added after Set must NOT leak into Config state")
}

func TestSet_DeepCopiesSliceValue(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	original := []any{"x", "y"}
	require.NoError(t, cfg.Set("user.list", original))

	original[0] = "MUTATED"

	got, err := cfg.Get("user.list")
	require.NoError(t, err)
	gotSlice, ok := got.([]any)
	require.True(t, ok,
		"value must round-trip as []any after deep copy")
	require.Len(t, gotSlice, 2)
	assert.Equal(t, "x", gotSlice[0],
		"Set must defensively deep-copy slice values ")
}

func TestSet_EmitsMetricsAndEvent(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	emitter := cfg.EnableEvents()
	metrics := cfg.EnableObservability()

	var setEvents atomic.Int64
	var changeEvents atomic.Int64
	emitter.On("set", func(args ...any) { setEvents.Add(1) })
	emitter.On("change", func(args ...any) { changeEvents.Add(1) })

	require.NoError(t, cfg.Set("database.host", "observed"))

	stats := metrics.Statistics()
	assert.Equal(t, 1, stats["set_count"],
		"successful Set must increment set_count (residual)")
	assert.Equal(t, 1, stats["change_count"],
		"successful Set must increment change_count")
	assert.Equal(t, int64(1), setEvents.Load(),
		"successful Set must emit a 'set' event (residual)")
	assert.Equal(t, int64(1), changeEvents.Load(),
		"successful Set must emit a 'change' event")
}

func TestSet_FailureEmitsSetFailedMetricAndEvent(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	emitter := cfg.EnableEvents()
	metrics := cfg.EnableObservability()

	var setFailures atomic.Int64
	var setSuccesses atomic.Int64
	emitter.On("set", func(args ...any) { setSuccesses.Add(1) })
	emitter.On("set_failed", func(args ...any) { setFailures.Add(1) })

	err = cfg.Set("debug.feature.flag", true)
	require.Error(t, err)

	stats := metrics.Statistics()
	assert.Equal(t, 0, stats["set_count"],
		"failed Set must NOT increment set_count")
	assert.Equal(t, 1, stats["set_failed_count"],
		"failed Set must increment set_failed_count")
	assert.Equal(t, int64(0), setSuccesses.Load(),
		"failed Set must NOT emit 'set'")
	assert.Equal(t, int64(1), setFailures.Load(),
		"failed Set must emit 'set_failed'")
}

func TestOverride_EmitsMetricsAndEvent(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	emitter := cfg.EnableEvents()
	metrics := cfg.EnableObservability()

	var overrideEvents atomic.Int64
	var restoreEvents atomic.Int64
	var changeEvents atomic.Int64
	emitter.On("override", func(args ...any) { overrideEvents.Add(1) })
	emitter.On("override_restored", func(args ...any) { restoreEvents.Add(1) })
	emitter.On("change", func(args ...any) { changeEvents.Add(1) })

	restore, err := cfg.Override(map[string]any{"database.host": "ovr"})
	require.NoError(t, err)
	require.NotNil(t, restore)
	restore()

	stats := metrics.Statistics()
	assert.Equal(t, 1, stats["override_count"],
		"successful Override must increment override_count")
	assert.Equal(t, 1, stats["override_restored_count"],
		"restore must increment override_restored_count")
	assert.GreaterOrEqual(t, stats["change_count"].(int), 2,
		"Override + restore must each increment change_count")
	assert.Equal(t, int64(1), overrideEvents.Load(),
		"successful Override must emit 'override' event")
	assert.Equal(t, int64(1), restoreEvents.Load(),
		"restore must emit 'override_restored' event")
	assert.GreaterOrEqual(t, changeEvents.Load(), int64(2),
		"Override + restore must each emit 'change'")
}

func TestOverride_FailureEmitsOverrideFailedMetricAndEvent(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	emitter := cfg.EnableEvents()
	metrics := cfg.EnableObservability()

	var overrideEvents atomic.Int64
	var failedEvents atomic.Int64
	emitter.On("override", func(args ...any) { overrideEvents.Add(1) })
	emitter.On("override_failed", func(args ...any) { failedEvents.Add(1) })

	_, err = cfg.Override(map[string]any{"debug.feature": "x"})
	require.Error(t, err)

	stats := metrics.Statistics()
	assert.Equal(t, 0, stats["override_count"],
		"failed Override must NOT increment override_count")
	assert.Equal(t, 1, stats["override_failed_count"],
		"failed Override must increment override_failed_count")
	assert.Equal(t, int64(0), overrideEvents.Load(),
		"failed Override must NOT emit 'override'")
	assert.Equal(t, int64(1), failedEvents.Load(),
		"failed Override must emit 'override_failed'")
}

func TestSet_SourceParityAcrossReload(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("database.host", "set-by-test"))
	mid := cfg.Explain("database.host")
	require.Equal(t, "runtime", mid["source"])

	require.NoError(t, cfg.ReloadWithContext(context.Background(),
		confii.WithIncremental(false),
	))

	post := cfg.Explain("database.host")
	require.Equal(t, true, post["exists"])
	assert.NotEqual(t, "runtime", post["source"],
		"Reload must overwrite the runtime source claim with the loader source ")
}
