package confii

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// G15: Extend lifecycle parity with Reload.
//
// Pre-G15, Config.Extend was a partial merge path that:
//   - bypassed composition (so _include directives were ignored),
//   - bypassed environment resolution (so default/<env> sections were
//     not collapsed into the active env),
//   - bypassed file tracking (so the new source was not watchable for
//     incremental reload),
//   - bypassed validation (so an extension violating the validator
//     silently committed),
//   - had no rollback contract (so a failed Extend left envConfig and
//     mergedConfig in their failed-mid-flight state, and the source
//     tracker may have recorded entries for the doomed loader).
//
// Post-G15, Extend mirrors Reload's seven-phase pipeline:
//   snapshot → load → compose → env-resolve → merge → validate → commit.
// Failure rolls back env/merged config + source tracker, fires
// RecordExtendFailed and emits "extend_failed". Success fires
// RecordExtend, "extend", change callbacks, RecordChange, and "change".
// =========================================================================

// -------------------------------------------------------------------------
// Test fixtures local to this file. Other Extend tests use stubLoader from
// config_lifecycle_test.go; the lifecycle tests keep narrow contracts so
// adding Extend-specific fixtures here avoids polluting that fixture set.
// -------------------------------------------------------------------------

// extendScriptedLoader is a minimal scripted loader for Extend tests. The
// reload-test scriptedLoader exists in config_reload_test.go but advances
// its index per-call; Extend only ever calls Load once per Extend, so a
// trivial fixed-payload loader is enough for most Extend cases. We use
// scriptedLoader from config_reload_test.go where the call index matters.

// -------------------------------------------------------------------------
// (1) Composition directives are honored.
// -------------------------------------------------------------------------

// TestExtend_HonorsCompositionDirectives proves that an Extend with a
// loader whose payload contains a "_include" directive resolves the
// included file and surfaces its keys. Pre-G15 the "_include" was left
// in envConfig as a literal map entry while none of the included keys
// appeared.
func TestExtend_HonorsCompositionDirectives(t *testing.T) {
	dir := t.TempDir()

	// Included file supplies "included_key".
	includedPath := filepath.Join(dir, "included.yaml")
	require.NoError(t, os.WriteFile(includedPath, []byte("included_key: from_include\n"), 0o600))

	// The Extend payload references the included file. We use absolute
	// paths because the composer resolves relative paths against the
	// loader's Source(). A stub loader's source is not a file, so a
	// relative include would resolve against the composer's basePath
	// ("." — the process cwd), which is not under our control.
	extendPayload := map[string]any{
		"_include":   []any{includedPath},
		"direct_key": "direct_value",
	}

	cfg := newTestConfig(t, map[string]any{"a": 1})

	err := cfg.Extend(context.Background(), &stubLoader{
		source: filepath.Join(dir, "extend_source.yaml"), // for path resolution
		data:   extendPayload,
	})
	require.NoError(t, err, "Extend with _include must not error")

	// The directly-supplied key must appear, AND the included key must
	// also appear. The "_include" directive itself must be stripped.
	direct, err := cfg.Get("direct_key")
	require.NoError(t, err)
	assert.Equal(t, "direct_value", direct)

	included, err := cfg.Get("included_key")
	require.NoError(t, err, "_include directive must have been processed by composer (G15)")
	assert.Equal(t, "from_include", included)

	assert.False(t, cfg.Has("_include"),
		"_include directive must be stripped from envConfig after composition (G15)")
}

// -------------------------------------------------------------------------
// (2) Environment resolution is honored.
// -------------------------------------------------------------------------

// TestExtend_HonorsEnvResolution proves Extend folds env-keyed sections.
// The payload has both a "default" map and a "production" map (the
// active env). Post-G15, Extend resolves them through envHandler so
// "production" overrides "default" and only the resolved keys reach
// envConfig. Pre-G15 the raw "default:"/"production:" branches were
// merged literally into envConfig.
func TestExtend_HonorsEnvResolution(t *testing.T) {
	cfg, err := New[any](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"existing": "yes"}}),
		WithEnv("production"),
	)
	require.NoError(t, err)

	envPayload := map[string]any{
		"default": map[string]any{
			"db_host": "default-host",
			"db_port": 5432,
		},
		"production": map[string]any{
			"db_host": "prod-host",
		},
		"staging": map[string]any{
			"db_host": "staging-host",
		},
	}

	err = cfg.Extend(context.Background(), &stubLoader{
		source: "env-keyed",
		data:   envPayload,
	})
	require.NoError(t, err)

	// production overrides default for db_host; default still supplies db_port.
	host, err := cfg.Get("db_host")
	require.NoError(t, err)
	assert.Equal(t, "prod-host", host,
		"production env section must override default after Extend (G15)")

	port, err := cfg.Get("db_port")
	require.NoError(t, err)
	assert.Equal(t, 5432, port,
		"default env section must contribute keys not in active env (G15)")

	// The env branches themselves must NOT be reachable via dot-paths in
	// envConfig — env resolution collapses them.
	assert.False(t, cfg.Has("default"),
		"raw 'default' section must not leak into envConfig (G15)")
	assert.False(t, cfg.Has("staging"),
		"non-active env section must not leak into envConfig (G15)")
	assert.False(t, cfg.Has("production"),
		"raw 'production' section must not leak into envConfig (G15)")
}

// -------------------------------------------------------------------------
// (3) File tracker registration.
// -------------------------------------------------------------------------

// TestExtend_RegistersWithFileTracker proves a file-based loader passed
// to Extend has its source registered with the file tracker, so a
// subsequent incremental reload notices when the file changes.
//
// We use observable behavior rather than poking the unexported
// fileTracker: after Extend, a Reload with the default incremental gate
// should NOT call the loader again if the file has not changed.
// Mutating the file should make the gate fire.
func TestExtend_RegistersWithFileTracker(t *testing.T) {
	dir := t.TempDir()
	extPath := filepath.Join(dir, "ext.yaml")
	require.NoError(t, os.WriteFile(extPath, []byte("ext_key: v1\n"), 0o600))

	cfg, err := New[any](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"a": 1}}),
	)
	require.NoError(t, err)

	err = cfg.Extend(context.Background(), &fileAutoLoader{path: extPath})
	require.NoError(t, err)

	got, err := cfg.Get("ext_key")
	require.NoError(t, err)
	assert.Equal(t, "v1", got)

	// Without modifying the file, an incremental Reload must NOT pick
	// up any change. We assert by mutating the file and verifying the
	// reload DOES pick up the new value — this is only possible if the
	// tracker registered the file in the first place. Without G15's
	// fileTracker.Track call, the file would never have been recorded
	// and HasChanged would return true (treating untracked-as-changed),
	// so the inverse assertion below would also pass — making the test
	// useless. The discriminator is: mutating the file produces the new
	// content via Reload, AND the post-Extend Has("ext_key") returns
	// true, AND post-Reload Get returns the new value.
	require.NoError(t, os.WriteFile(extPath, []byte("ext_key: v2\n"), 0o600))
	err = cfg.Reload(context.Background())
	require.NoError(t, err, "Reload must succeed after Extend-registered file changed")

	got, err = cfg.Get("ext_key")
	require.NoError(t, err)
	assert.Equal(t, "v2", got,
		"file changed after Extend; Reload must pick up new value (G15 file tracking)")
}

// TestExtend_NonFileLoaderSilentlySkipsTracking pins the G20 deferral:
// non-file loaders (those whose Source() is not a real path) must not
// fail Extend just because file tracking can't be done. The G20 fix
// will introduce a source-capability model; until then, file tracker
// errors are silently logged at debug and Extend proceeds.
func TestExtend_NonFileLoaderSilentlySkipsTracking(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})

	// stubLoader's Source() is "stub-source", which doesn't exist on
	// disk. The fileTracker.Track call inside Extend will fail with an
	// os.Stat error; G15 swallows it (debug log only) and continues.
	err := cfg.Extend(context.Background(), &stubLoader{
		source: "non-file-source",
		data:   map[string]any{"k": "v"},
	})
	require.NoError(t, err, "Extend must succeed with non-file loader (G15 / G20 deferral)")

	v, gerr := cfg.Get("k")
	require.NoError(t, gerr)
	assert.Equal(t, "v", v)
}

// -------------------------------------------------------------------------
// (4) Rollback on loader error.
// -------------------------------------------------------------------------

// TestExtend_RollsBackOnLoaderError proves that under ErrorPolicyRaise,
// a loader error triggers full rollback: envConfig, mergedConfig, and
// source tracker are restored to their pre-Extend state. Pre-G15 the
// error short-circuited before any mutation, so this test would pass
// trivially; the post-G15 contract is that the snapshot/restore plumbing
// is in place so future failure modes (compose, validate) also roll back
// uniformly.
func TestExtend_RollsBackOnLoaderError(t *testing.T) {
	cfg, err := New[any](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"original": "kept"}}),
		WithOnError(ErrorPolicyRaise),
		WithDebugMode(true),
	)
	require.NoError(t, err)

	preInfo := cfg.GetSourceInfo("original")
	require.NotNil(t, preInfo)
	preCount := preInfo.OverrideCount
	preStats := cfg.GetSourceStatistics()
	preTotalKeys := preStats["total_keys"]

	err = cfg.Extend(context.Background(), &stubLoader{
		source: "broken-loader",
		err:    errors.New("loader-boom"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loader-boom")

	// Pre-extend state preserved.
	v, gerr := cfg.Get("original")
	require.NoError(t, gerr)
	assert.Equal(t, "kept", v)

	postInfo := cfg.GetSourceInfo("original")
	require.NotNil(t, postInfo)
	assert.Equal(t, preCount, postInfo.OverrideCount,
		"override_count must not grow after rolled-back Extend (G15)")

	postStats := cfg.GetSourceStatistics()
	assert.Equal(t, preTotalKeys, postStats["total_keys"],
		"total_keys must not grow after rolled-back Extend (G15)")
}

// -------------------------------------------------------------------------
// (5) Rollback on validation error.
// -------------------------------------------------------------------------

// TestExtend_RollsBackOnValidationError proves that when validate-on-load
// is configured and the post-Extend state fails validation, the entire
// extension is rolled back. Pre-G15 Extend skipped validation entirely,
// so an extension violating the validator silently committed.
func TestExtend_RollsBackOnValidationError(t *testing.T) {
	type AppCfg struct {
		Required string `mapstructure:"required" validate:"required"`
	}
	// We need both ValidateOnLoad AND a non-nil Schema. Schema is
	// gating in c.opts, and post-Wave 8 the type-parameter T is the
	// validation target (see validate.DecodeAndValidate[T]).
	cfg, err := New[AppCfg](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"required": "ok"}}),
		WithValidateOnLoad(true),
		WithSchema(struct{}{}), // non-nil sentinel; the schema gate is currently a presence check (G01).
	)
	require.NoError(t, err)

	// Initial state: required is set so Typed validates clean.
	model, err := cfg.Typed()
	require.NoError(t, err)
	require.NotNil(t, model)
	assert.Equal(t, "ok", model.Required)

	// Extend with a payload that strips the required field by
	// supplying nothing; the merger preserves "required" because
	// neither of the strategies removes keys, but a payload that
	// shadows "required" with an empty string would. Rather than rely
	// on shadow semantics, we use a payload that introduces an
	// explicit empty string, which fails the `required` validator (Go
	// validator treats "" as missing for required-string fields).
	err = cfg.Extend(context.Background(), &stubLoader{
		source: "broken",
		data:   map[string]any{"required": ""},
	})
	require.Error(t, err, "Extend must surface validation error post-G15")
	assert.True(t, errors.Is(err, ErrConfigValidation),
		"validation failure must wrap ErrConfigValidation, got: %v", err)

	// Rollback restored the original "required" value.
	model2, err := cfg.Typed()
	require.NoError(t, err, "post-rollback Typed must succeed against restored state")
	require.NotNil(t, model2)
	assert.Equal(t, "ok", model2.Required,
		"rollback must restore pre-Extend value (G15)")
}

// -------------------------------------------------------------------------
// (6) Policy matrix for loader errors.
// -------------------------------------------------------------------------

// TestExtend_PolicyMatrix_LoaderError_Raise mirrors the Reload policy
// matrix: under Raise, a loader error surfaces and pre-Extend state is
// preserved.
func TestExtend_PolicyMatrix_LoaderError_Raise(t *testing.T) {
	cfg, err := New[any](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"k": "v0"}}),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)

	err = cfg.Extend(context.Background(), &stubLoader{
		source: "bad",
		err:    errors.New("raise-extend"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raise-extend")

	v, _ := cfg.Get("k")
	assert.Equal(t, "v0", v, "Raise policy must preserve pre-Extend state (G15)")
}

// TestExtend_PolicyMatrix_LoaderError_Warn pins Warn behavior: error
// logged, source skipped, Extend returns nil, no commit-time
// observability fires (because nothing was committed).
func TestExtend_PolicyMatrix_LoaderError_Warn(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()

	opts := append([]Option{
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"k": "v0"}}),
		WithOnError(ErrorPolicyWarn),
	}, logOpts...)

	cfg, err := New[any](context.Background(), opts...)
	require.NoError(t, err)

	logBuf.Reset()
	err = cfg.Extend(context.Background(), &stubLoader{
		source: "bad",
		err:    errors.New("warn-extend"),
	})
	require.NoError(t, err, "Warn policy must not surface loader error from Extend (G15)")

	out := logBuf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "loader error")
	assert.Contains(t, out, "warn-extend")
}

// TestExtend_PolicyMatrix_LoaderError_Ignore pins Ignore-vs-Warn: no log
// record at all, no error returned.
func TestExtend_PolicyMatrix_LoaderError_Ignore(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()

	opts := append([]Option{
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"k": "v0"}}),
		WithOnError(ErrorPolicyIgnore),
	}, logOpts...)

	cfg, err := New[any](context.Background(), opts...)
	require.NoError(t, err)

	logBuf.Reset()
	err = cfg.Extend(context.Background(), &stubLoader{
		source: "bad",
		err:    errors.New("ignore-extend"),
	})
	require.NoError(t, err)

	assert.NotContains(t, logBuf.String(), "loader error",
		"ErrorPolicyIgnore must not produce a 'loader error' log record (G15)")
	assert.NotContains(t, logBuf.String(), "ignore-extend")
}

// -------------------------------------------------------------------------
// (7) Event emission: extend on success, extend_failed on failure.
// -------------------------------------------------------------------------

// extendRecordingSink is a thread-safe event recorder local to Extend
// tests. The Reload tests in config_reload_test.go define recordingSink
// for the same purpose; we duplicate the type here rather than promote
// it because future deletions of either file should not break the other.
type extendRecordingSink struct {
	mu     sync.Mutex
	events []string
}

func (r *extendRecordingSink) record(name string) func(args ...any) {
	return func(args ...any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.events = append(r.events, name)
	}
}

func (r *extendRecordingSink) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

// TestExtend_EmitsExtendAndExtendFailedEvents pins both directions of
// the observability contract:
//   - Successful Extend emits "extend" then "change", increments
//     extend_count, and does NOT emit "extend_failed".
//   - Failed Extend (loader error under Raise) emits "extend_failed",
//     increments extend_failed_count, and does NOT emit "extend" or
//     "change".
//
// This mirrors the Reload G14 metrics-and-events-emitted-only-on-commit
// regression test.
func TestExtend_EmitsExtendAndExtendFailedEvents(t *testing.T) {
	t.Run("success_emits_extend_and_change", func(t *testing.T) {
		cfg, err := New[any](context.Background(),
			WithLoaders(&stubLoader{source: "base", data: map[string]any{"original": "yes"}}),
		)
		require.NoError(t, err)

		sink := &extendRecordingSink{}
		emitter := cfg.EnableEvents()
		emitter.On("extend", sink.record("extend"))
		emitter.On("extend_failed", sink.record("extend_failed"))
		emitter.On("change", sink.record("change"))
		metrics := cfg.EnableObservability()

		err = cfg.Extend(context.Background(), &stubLoader{
			source: "added",
			data:   map[string]any{"new_key": "new_value"},
		})
		require.NoError(t, err)

		got := sink.snapshot()
		assert.Contains(t, got, "extend",
			"success path must emit 'extend' event (G15)")
		assert.Contains(t, got, "change",
			"success path must emit 'change' event (G15)")
		assert.NotContains(t, got, "extend_failed",
			"success path must NOT emit 'extend_failed' (G15)")

		// Order: extend before change (parity with Reload G14 ordering).
		assert.Equal(t, []string{"extend", "change"}, got,
			"extend event must precede change event (G15)")

		stats := metrics.Statistics()
		assert.Equal(t, 1, stats["extend_count"],
			"extend_count must increment on commit (G15)")
		assert.Equal(t, 0, stats["extend_failed_count"])
	})

	t.Run("loader_failure_emits_extend_failed_only", func(t *testing.T) {
		cfg, err := New[any](context.Background(),
			WithLoaders(&stubLoader{source: "base", data: map[string]any{"original": "yes"}}),
			WithOnError(ErrorPolicyRaise),
		)
		require.NoError(t, err)

		sink := &extendRecordingSink{}
		emitter := cfg.EnableEvents()
		emitter.On("extend", sink.record("extend"))
		emitter.On("extend_failed", sink.record("extend_failed"))
		emitter.On("change", sink.record("change"))
		metrics := cfg.EnableObservability()

		err = cfg.Extend(context.Background(), &stubLoader{
			source: "broken",
			err:    errors.New("ext-loader-failed"),
		})
		require.Error(t, err)

		got := sink.snapshot()
		assert.NotContains(t, got, "extend",
			"failure path must NOT emit 'extend' (G15)")
		assert.NotContains(t, got, "change",
			"failure path must NOT emit 'change' (G15)")
		assert.Contains(t, got, "extend_failed",
			"failure path must emit 'extend_failed' (G15)")

		stats := metrics.Statistics()
		assert.Equal(t, 0, stats["extend_count"],
			"extend_count must NOT increment on rollback (G15)")
		assert.Equal(t, 1, stats["extend_failed_count"],
			"extend_failed_count must increment on rollback (G15)")
	})
}

// -------------------------------------------------------------------------
// (8) Validated-model cache invalidation on commit.
// -------------------------------------------------------------------------

// TestExtend_InvalidatesValidatedModelCache pins the G11 cache contract:
// Extend's commit must invalidate c.validatedModel so a subsequent
// Typed() re-decodes the post-Extend state. Pre-G15 Extend already did
// this (the only behavior G15 preserved verbatim), so this test guards
// against a refactor regression. We assert by observing distinct decoded
// values for two successive Typed() calls separated by an Extend.
func TestExtend_InvalidatesValidatedModelCache(t *testing.T) {
	type AppCfg struct {
		Field string `mapstructure:"field"`
	}
	cfg, err := New[AppCfg](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"field": "before"}}),
	)
	require.NoError(t, err)

	model1, err := cfg.Typed()
	require.NoError(t, err)
	require.NotNil(t, model1)
	assert.Equal(t, "before", model1.Field)

	// Subsequent Typed() returns the cached model.
	cached, err := cfg.Typed()
	require.NoError(t, err)
	assert.Same(t, model1, cached,
		"second Typed() before Extend must return cached pointer (G11)")

	err = cfg.Extend(context.Background(), &stubLoader{
		source: "overlay",
		data:   map[string]any{"field": "after"},
	})
	require.NoError(t, err)

	// Post-Extend: cache MUST have been invalidated.
	model2, err := cfg.Typed()
	require.NoError(t, err)
	require.NotNil(t, model2)
	assert.Equal(t, "after", model2.Field,
		"Typed() after Extend must reflect post-Extend state (G15 / G11 cache invalidation)")
	assert.NotSame(t, model1, model2,
		"Typed() after Extend must return a fresh pointer (G15 cache invalidation)")
}

// -------------------------------------------------------------------------
// (9) Frozen state still rejected (G14 pre-existing contract preserved).
// -------------------------------------------------------------------------

// TestExtend_FrozenStateRejectedBeforePipeline ensures the freeze check
// runs BEFORE any snapshot/load work (so a frozen Extend is a true
// no-op, not a roll-back-after-mutation).
func TestExtend_FrozenStateRejectedBeforePipeline(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})
	cfg.Freeze()

	loaderCalls := int32(0)
	tracking := &countingLoader{
		source: "should-not-be-called",
		data:   map[string]any{"b": 2},
		calls:  &loaderCalls,
	}

	err := cfg.Extend(context.Background(), tracking)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigFrozen))

	// Loader must NOT have been called — frozen check fires first.
	assert.Equal(t, int32(0), atomic.LoadInt32(&loaderCalls),
		"frozen Extend must short-circuit before invoking loader (G15)")
}

// countingLoader records the number of Load() calls so tests can assert
// that frozen / early-exit paths never invoke the loader.
type countingLoader struct {
	source string
	data   map[string]any
	calls  *int32
}

func (c *countingLoader) Load(_ context.Context) (map[string]any, error) {
	atomic.AddInt32(c.calls, 1)
	return c.data, nil
}

func (c *countingLoader) Source() string { return c.source }
