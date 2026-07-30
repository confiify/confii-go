// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

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

func TestExtend_HonorsCompositionDirectives(t *testing.T) {
	dir := t.TempDir()

	includedPath := filepath.Join(dir, "included.yaml")
	require.NoError(t, os.WriteFile(includedPath, []byte("included_key: from_include\n"), 0o600))

	extendPayload := map[string]any{
		"_include":   []any{includedPath},
		"direct_key": "direct_value",
	}

	cfg := newTestConfig(t, map[string]any{"a": 1})

	err := cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: filepath.Join(dir, "extend_source.yaml"),
		data:   extendPayload,
	})
	require.NoError(t, err, "Extend with _include must not error")

	direct, err := cfg.Get("direct_key")
	require.NoError(t, err)
	assert.Equal(t, "direct_value", direct)

	included, err := cfg.Get("included_key")
	require.NoError(t, err, "_include directive must have been processed by composer ")
	assert.Equal(t, "from_include", included)

	assert.False(t, cfg.Has("_include"),
		"_include directive must be stripped from envConfig after composition ")
}

func TestExtend_HonorsEnvResolution(t *testing.T) {
	cfg, err := NewWithContext[any](context.Background(),
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

	err = cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "env-keyed",
		data:   envPayload,
	})
	require.NoError(t, err)

	host, err := cfg.Get("db_host")
	require.NoError(t, err)
	assert.Equal(t, "prod-host", host,
		"production env section must override default after Extend ")

	port, err := cfg.Get("db_port")
	require.NoError(t, err)
	assert.Equal(t, 5432, port,
		"default env section must contribute keys not in active env ")

	assert.False(t, cfg.Has("default"),
		"raw 'default' section must not leak into envConfig ")
	assert.False(t, cfg.Has("staging"),
		"non-active env section must not leak into envConfig ")
	assert.False(t, cfg.Has("production"),
		"raw 'production' section must not leak into envConfig ")
}

func TestExtend_RegistersWithFileTracker(t *testing.T) {
	dir := t.TempDir()
	extPath := filepath.Join(dir, "ext.yaml")
	require.NoError(t, os.WriteFile(extPath, []byte("ext_key: v1\n"), 0o600))

	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"a": 1}}),
	)
	require.NoError(t, err)

	err = cfg.ExtendWithContext(context.Background(), &fileAutoLoader{path: extPath})
	require.NoError(t, err)

	got, err := cfg.Get("ext_key")
	require.NoError(t, err)
	assert.Equal(t, "v1", got)

	require.NoError(t, os.WriteFile(extPath, []byte("ext_key: v2\n"), 0o600))
	err = cfg.ReloadWithContext(context.Background())
	require.NoError(t, err, "Reload must succeed after Extend-registered file changed")

	got, err = cfg.Get("ext_key")
	require.NoError(t, err)
	assert.Equal(t, "v2", got,
		"file changed after Extend; Reload must pick up new value (file tracking)")
}

func TestExtend_NonFileLoaderSilentlySkipsTracking(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})

	err := cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "non-file-source",
		data:   map[string]any{"k": "v"},
	})
	require.NoError(t, err, "Extend must succeed with non-file loader (/  deferral)")

	v, gerr := cfg.Get("k")
	require.NoError(t, gerr)
	assert.Equal(t, "v", v)
}

func TestExtend_RollsBackOnLoaderError(t *testing.T) {
	cfg, err := NewWithContext[any](context.Background(),
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

	err = cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "broken-loader",
		err:    errors.New("loader-boom"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loader-boom")

	v, gerr := cfg.Get("original")
	require.NoError(t, gerr)
	assert.Equal(t, "kept", v)

	postInfo := cfg.GetSourceInfo("original")
	require.NotNil(t, postInfo)
	assert.Equal(t, preCount, postInfo.OverrideCount,
		"override_count must not grow after rolled-back Extend ")

	postStats := cfg.GetSourceStatistics()
	assert.Equal(t, preTotalKeys, postStats["total_keys"],
		"total_keys must not grow after rolled-back Extend ")
}

func TestExtend_RollsBackOnValidationError(t *testing.T) {
	type AppCfg struct {
		Required string `confii:"required" validate:"required"`
	}
	cfg, err := NewWithContext[AppCfg](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"required": "ok"}}),
		WithValidateOnLoad(true),
		WithSchema(struct{}{}))
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	require.NotNil(t, model)
	assert.Equal(t, "ok", model.Required)

	err = cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "broken",
		data:   map[string]any{"required": ""},
	})
	require.Error(t, err, "Extend must surface validation error post-")
	assert.True(t, errors.Is(err, ErrConfigValidation),
		"validation failure must wrap ErrConfigValidation, got: %v", err)

	model2, err := cfg.Typed()
	require.NoError(t, err, "post-rollback Typed must succeed against restored state")
	require.NotNil(t, model2)
	assert.Equal(t, "ok", model2.Required,
		"rollback must restore pre-Extend value ")
}

func TestExtend_PolicyMatrix_LoaderError_Raise(t *testing.T) {
	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"k": "v0"}}),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)

	err = cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "bad",
		err:    errors.New("raise-extend"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raise-extend")

	v, _ := cfg.Get("k")
	assert.Equal(t, "v0", v, "Raise policy must preserve pre-Extend state ")
}

func TestExtend_PolicyMatrix_LoaderError_Warn(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()

	opts := append([]Option{
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"k": "v0"}}),
		WithOnError(ErrorPolicyWarn),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)

	logBuf.Reset()
	err = cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "bad",
		err:    errors.New("warn-extend"),
	})
	require.NoError(t, err, "Warn policy must not surface loader error from Extend ")

	out := logBuf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "loader error")
	assert.Contains(t, out, "warn-extend")
}

func TestExtend_PolicyMatrix_LoaderError_Ignore(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()

	opts := append([]Option{
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"k": "v0"}}),
		WithOnError(ErrorPolicyIgnore),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)

	logBuf.Reset()
	err = cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "bad",
		err:    errors.New("ignore-extend"),
	})
	require.NoError(t, err)

	assert.NotContains(t, logBuf.String(), "loader error",
		"ErrorPolicyIgnore must not produce a 'loader error' log record ")
	assert.NotContains(t, logBuf.String(), "ignore-extend")
}

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

func TestExtend_EmitsExtendAndExtendFailedEvents(t *testing.T) {
	t.Run("success_emits_extend_and_change", func(t *testing.T) {
		cfg, err := NewWithContext[any](context.Background(),
			WithLoaders(&stubLoader{source: "base", data: map[string]any{"original": "yes"}}),
		)
		require.NoError(t, err)

		sink := &extendRecordingSink{}
		emitter := cfg.EnableEvents()
		emitter.On("extend", sink.record("extend"))
		emitter.On("extend_failed", sink.record("extend_failed"))
		emitter.On("change", sink.record("change"))
		metrics := cfg.EnableObservability()

		err = cfg.ExtendWithContext(context.Background(), &stubLoader{
			source: "added",
			data:   map[string]any{"new_key": "new_value"},
		})
		require.NoError(t, err)

		got := sink.snapshot()
		assert.Contains(t, got, "extend",
			"success path must emit 'extend' event ")
		assert.Contains(t, got, "change",
			"success path must emit 'change' event ")
		assert.NotContains(t, got, "extend_failed",
			"success path must NOT emit 'extend_failed' ")

		assert.Equal(t, []string{"extend", "change"}, got,
			"extend event must precede change event ")

		stats := metrics.Statistics()
		assert.Equal(t, 1, stats["extend_count"],
			"extend_count must increment on commit ")
		assert.Equal(t, 0, stats["extend_failed_count"])
	})

	t.Run("loader_failure_emits_extend_failed_only", func(t *testing.T) {
		cfg, err := NewWithContext[any](context.Background(),
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

		err = cfg.ExtendWithContext(context.Background(), &stubLoader{
			source: "broken",
			err:    errors.New("ext-loader-failed"),
		})
		require.Error(t, err)

		got := sink.snapshot()
		assert.NotContains(t, got, "extend",
			"failure path must NOT emit 'extend' ")
		assert.NotContains(t, got, "change",
			"failure path must NOT emit 'change' ")
		assert.Contains(t, got, "extend_failed",
			"failure path must emit 'extend_failed' ")

		stats := metrics.Statistics()
		assert.Equal(t, 0, stats["extend_count"],
			"extend_count must NOT increment on rollback ")
		assert.Equal(t, 1, stats["extend_failed_count"],
			"extend_failed_count must increment on rollback ")
	})
}

func TestExtend_InvalidatesValidatedModelCache(t *testing.T) {
	type AppCfg struct {
		Field string `confii:"field"`
	}
	cfg, err := NewWithContext[AppCfg](context.Background(),
		WithLoaders(&stubLoader{source: "base", data: map[string]any{"field": "before"}}),
	)
	require.NoError(t, err)

	model1, err := cfg.Typed()
	require.NoError(t, err)
	require.NotNil(t, model1)
	assert.Equal(t, "before", model1.Field)

	cached, err := cfg.Typed()
	require.NoError(t, err)
	assert.Same(t, model1, cached,
		"second Typed before Extend must return cached pointer ")

	err = cfg.ExtendWithContext(context.Background(), &stubLoader{
		source: "overlay",
		data:   map[string]any{"field": "after"},
	})
	require.NoError(t, err)

	model2, err := cfg.Typed()
	require.NoError(t, err)
	require.NotNil(t, model2)
	assert.Equal(t, "after", model2.Field,
		"Typed after Extend must reflect post-Extend state (/  cache invalidation)")
	assert.NotSame(t, model1, model2,
		"Typed after Extend must return a fresh pointer (cache invalidation)")
}

func TestExtend_FrozenStateRejectedBeforePipeline(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})
	cfg.Freeze()

	loaderCalls := int32(0)
	tracking := &countingLoader{
		source: "should-not-be-called",
		data:   map[string]any{"b": 2},
		calls:  &loaderCalls,
	}

	err := cfg.ExtendWithContext(context.Background(), tracking)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigFrozen))

	assert.Equal(t, int32(0), atomic.LoadInt32(&loaderCalls),
		"frozen Extend must short-circuit before invoking loader ")
}

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
