package confii_test

// G12 / Wave 11 coverage for runtime mutation APIs (Set, Override).
//
// Pre-G12 the runtime mutation APIs left state and introspection
// inconsistent in four orthogonal ways:
//
//  1. Set updated envConfig/mergedConfig but did not update the source
//     tracker, so a key written via Set still reported its pre-Set
//     source via Explain.
//  2. Set silently swallowed dictutil.SetNested *PathError errors, so
//     a caller's bad path traversal "succeeded" while leaving state
//     unchanged.
//  3. Override silently swallowed the same SetNested errors AND always
//     returned nil, so partial-application bugs were invisible.
//  4. Set did not fire OnChange callbacks (F-G12-Set-CallbackSilence).
//  5. Set stored caller's value reference without defensive copy
//     (F-G10-SetInputAlias) — a caller mutating the original map after
//     Set bled into Config state.
//  6. G21 residual: Set / Override emitted no metrics or events, so
//     observers could not see runtime mutations.
//
// Each test below pins one observable that would fail under the
// pre-G12 behavior.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSet_SourceTrackingParity asserts that a successful Set updates
// the source tracker so Explain reports source="runtime" for the
// runtime-written key. Pre-G12 Explain reported the loader's source
// (e.g. "loader/testdata/simple.yaml") even after Set overrode the
// value, leaving introspection inconsistent with the live state.
func TestSet_SourceTrackingParity(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Sanity: pre-Set source is the file loader.
	pre := cfg.Explain("database.host")
	require.Equal(t, true, pre["exists"])
	require.NotEqual(t, "runtime", pre["source"],
		"fixture precondition: pre-Set source must be the file loader")

	require.NoError(t, cfg.Set("database.host", "set-by-runtime"))

	post := cfg.Explain("database.host")
	require.Equal(t, true, post["exists"])
	assert.Equal(t, "runtime", post["source"],
		"Explain must report source=runtime after Set (G12 source-tracking parity)")
	assert.Equal(t, "runtime", post["loader_type"],
		"Explain must report loader_type=runtime after Set (G12 source-tracking parity)")
	assert.Equal(t, "set-by-runtime", post["current_value"])
}

// TestSet_SourceTrackingParity_NewKey asserts that Set adding a brand
// new key (not present in any loader) records "runtime" as the source.
func TestSet_SourceTrackingParity_NewKey(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("runtime.added.key", 42))

	got := cfg.Explain("runtime.added.key")
	require.Equal(t, true, got["exists"])
	assert.Equal(t, "runtime", got["source"])
	assert.Equal(t, 42, got["current_value"])
}

// TestSet_PathErrorPropagatesAsTypedError asserts that when Set's
// dot-separated key path traverses through a non-map intermediate,
// the *PathError is surfaced as a typed *ConfigError wrapping
// ErrConfigInvalid. Pre-G12 the error was silently swallowed, so the
// caller saw success while state stayed unchanged.
func TestSet_PathErrorPropagatesAsTypedError(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// "debug" is bound to a bool in simple.yaml, so "debug.feature.flag"
	// must traverse through a non-map intermediate. Pre-G12 SetNested
	// returned its *PathError and Set silently swallowed it.
	err = cfg.Set("debug.feature.flag", true)
	require.Error(t, err, "Set must surface the SetNested PathError (G12)")

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		"expected *ConfigError, got %T (%v)", err, err)
	assert.Equal(t, "Set", ce.Op,
		"ConfigError.Op must be Set (G12 typed-error contract)")
	assert.Equal(t, "debug.feature.flag", ce.Key,
		"ConfigError.Key must echo the rejected key path")
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"error must wrap ErrConfigInvalid sentinel (G12)")

	// State must be unchanged: original "debug" still reachable as bool.
	dbg, gerr := cfg.GetBool("debug")
	require.NoError(t, gerr,
		"after a rejected Set, the pre-Set debug value must still be queryable")
	assert.Equal(t, true, dbg)
}

// TestOverride_PathErrorPropagatesAsTypedError asserts that when an
// override key's path traverses through a non-map intermediate, the
// *PathError is surfaced as a typed *ConfigError wrapping
// ErrConfigInvalid AND that the partial mutations applied earlier in
// the loop are rolled back. Pre-G12 the error was silently swallowed
// and Override always returned (restore, nil).
func TestOverride_PathErrorPropagatesAsTypedError(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// "debug" is a bool; "debug.feature" forces a non-map traversal.
	restore, err := cfg.Override(map[string]any{
		"debug.feature": "x",
	})
	require.Error(t, err,
		"Override must surface SetNested PathError (G12)")
	assert.Nil(t, restore,
		"Override must return nil restore on failure")

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, "Override", ce.Op)
	assert.Equal(t, "debug.feature", ce.Key)
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"Override error must wrap ErrConfigInvalid sentinel")

	// Live state must be untouched: debug stays bool=true.
	dbg, gerr := cfg.GetBool("debug")
	require.NoError(t, gerr)
	assert.Equal(t, true, dbg)
}

// TestSet_FiresOnChangeCallback_AddAndUpdate pins the
// F-G12-Set-CallbackSilence contract: Set fires OnChange callbacks
// with (oldVal, newVal) when the value changes, and (nil, newVal)
// for a brand new key.
func TestSet_FiresOnChangeCallback_AddAndUpdate(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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

	// Update existing key: must fire (oldVal != nil, newVal != nil).
	require.NoError(t, cfg.Set("database.host", "set-by-test"))
	// Add new key: must fire (oldVal == nil, newVal != nil).
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
		"Set on existing key must fire OnChange (F-G12-Set-CallbackSilence)")
	assert.False(t, update.oldNil,
		"update fire must report non-nil oldVal")
	assert.False(t, update.newNil,
		"update fire must report non-nil newVal")
	assert.Equal(t, "localhost", update.oldS)
	assert.Equal(t, "set-by-test", update.newS)

	require.NotNil(t, addition,
		"Set on new key must fire OnChange (F-G12-Set-CallbackSilence)")
	assert.True(t, addition.oldNil,
		"addition fire must report nil oldVal (G13 deletion contract symmetry)")
	assert.False(t, addition.newNil)
	assert.Equal(t, "added", addition.newS)
}

// TestSet_DeepCopiesMapValue pins F-G10-SetInputAlias: a caller
// passing a map[string]any to Set, then mutating that map after the
// Set returns, must NOT bleed into Config state. Pre-G10 Set stored
// the caller's map by reference, inverting the read-time deep-copy
// contract introduced in G11.
func TestSet_DeepCopiesMapValue(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	original := map[string]any{"a": 1}
	require.NoError(t, cfg.Set("user.payload", original))

	// Mutate caller's reference. Pre-G10 this would alias into c.envConfig.
	original["a"] = 999
	original["b"] = "leaked"

	// Confii must observe the value at the moment of Set.
	got, err := cfg.Get("user.payload.a")
	require.NoError(t, err)
	assert.Equal(t, 1, got,
		"Set must defensively deep-copy map values (F-G10-SetInputAlias)")

	// And the leaked sibling must not be reachable.
	_, err = cfg.Get("user.payload.b")
	assert.Error(t, err,
		"caller-side keys added after Set must NOT leak into Config state")
}

// TestSet_DeepCopiesSliceValue is the slice counterpart: a []any
// passed to Set must be deep-copied so caller mutation does not bleed.
func TestSet_DeepCopiesSliceValue(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	original := []any{"x", "y"}
	require.NoError(t, cfg.Set("user.list", original))

	// Mutate the slice's storage in place.
	original[0] = "MUTATED"

	got, err := cfg.Get("user.list")
	require.NoError(t, err)
	gotSlice, ok := got.([]any)
	require.True(t, ok,
		"value must round-trip as []any after deep copy")
	require.Len(t, gotSlice, 2)
	assert.Equal(t, "x", gotSlice[0],
		"Set must defensively deep-copy slice values (F-G10-SetInputAlias)")
}

// TestSet_EmitsMetricsAndEvent pins the G21 residual scope:
// a successful Set must emit RecordSet + RecordChange and "set" +
// "change" events. Pre-G21-residual Set was invisible to observers.
func TestSet_EmitsMetricsAndEvent(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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
		"successful Set must increment set_count (G21 residual)")
	assert.Equal(t, 1, stats["change_count"],
		"successful Set must increment change_count")
	assert.Equal(t, int64(1), setEvents.Load(),
		"successful Set must emit a 'set' event (G21 residual)")
	assert.Equal(t, int64(1), changeEvents.Load(),
		"successful Set must emit a 'change' event")
}

// TestSet_FailureEmitsSetFailedMetricAndEvent: a Set that hits a
// non-map traversal must fire the failure-path observability and
// leave set_count untouched. Mirrors the reload_failed / extend_failed
// success-vs-failure split.
func TestSet_FailureEmitsSetFailedMetricAndEvent(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	emitter := cfg.EnableEvents()
	metrics := cfg.EnableObservability()

	var setFailures atomic.Int64
	var setSuccesses atomic.Int64
	emitter.On("set", func(args ...any) { setSuccesses.Add(1) })
	emitter.On("set_failed", func(args ...any) { setFailures.Add(1) })

	err = cfg.Set("debug.feature.flag", true) // debug is bool
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

// TestOverride_EmitsMetricsAndEvent pins that a successful Override
// emits RecordOverride + RecordChange and "override" + "change"
// events. Pre-G21-residual Override was invisible to observers.
func TestOverride_EmitsMetricsAndEvent(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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
		"restore() must increment override_restored_count")
	assert.GreaterOrEqual(t, stats["change_count"].(int), 2,
		"Override + restore must each increment change_count")
	assert.Equal(t, int64(1), overrideEvents.Load(),
		"successful Override must emit 'override' event")
	assert.Equal(t, int64(1), restoreEvents.Load(),
		"restore() must emit 'override_restored' event")
	assert.GreaterOrEqual(t, changeEvents.Load(), int64(2),
		"Override + restore must each emit 'change'")
}

// TestOverride_FailureEmitsOverrideFailedMetricAndEvent pins the
// failure-path observability: when an override key hits a non-map
// traversal, override_failed_count and the "override_failed" event
// must fire while override_count stays at zero.
func TestOverride_FailureEmitsOverrideFailedMetricAndEvent(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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

// TestSet_SourceParityAcrossReload pins the second half of the
// source-tracking parity contract: a Set source claim must persist
// only until a subsequent Reload overwrites it. After a Reload that
// re-reads the file, the source must revert to the file source.
func TestSet_SourceParityAcrossReload(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("database.host", "set-by-test"))
	mid := cfg.Explain("database.host")
	require.Equal(t, "runtime", mid["source"])

	require.NoError(t, cfg.Reload(context.Background(),
		confii.WithIncremental(false),
	))

	post := cfg.Explain("database.host")
	require.Equal(t, true, post["exists"])
	assert.NotEqual(t, "runtime", post["source"],
		"Reload must overwrite the runtime source claim with the loader source (G12)")
}
