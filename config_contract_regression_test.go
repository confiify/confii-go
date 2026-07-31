// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/confiify/confii-go/v2/compose"
	"github.com/confiify/confii-go/v2/envhandler"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/confiify/confii-go/v2/sourcetrack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancelOnErrCheckContext struct {
	context.Context
	cancel context.CancelFunc
	at     int32
	calls  atomic.Int32
}

func (c *cancelOnErrCheckContext) Err() error {
	if c.calls.Add(1) == c.at {
		c.cancel()
	}
	return c.Context.Err()
}

func TestContextAwareReadsRejectInvalidContexts(t *testing.T) {
	type model struct {
		Value string `confii:"value"`
	}
	cfg, err := New[model](WithLoaders(&stubLoader{source: "context-reads", data: map[string]any{"value": "ready"}}))
	require.NoError(t, err)

	var nilContext context.Context
	_, err = cfg.ToDictWithContext(nilContext)
	require.ErrorIs(t, err, ErrConfigInvalid)
	_, err = cfg.TypedWithContext(nilContext)
	require.ErrorIs(t, err, ErrConfigValidation)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cfg.ToDictWithContext(canceled)
	require.ErrorIs(t, err, context.Canceled)
	_, err = cfg.TypedWithContext(canceled)
	require.ErrorIs(t, err, context.Canceled)

	typed, err := cfg.TypedWithContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ready", typed.Value)
}

func TestTypedReportsDecodeAndValidationFailures(t *testing.T) {
	type model struct {
		Required string `confii:"required" validate:"required"`
	}
	cfg, err := New[model](
		WithLoaders(&stubLoader{source: "typed-invalid", data: map[string]any{}}),
		WithValidateOnLoad(false),
	)
	require.NoError(t, err)
	_, err = cfg.TypedWithContext(context.Background())
	require.ErrorIs(t, err, ErrConfigValidation)
}

func TestGetIntRejectsPlatformOverflow(t *testing.T) {
	if strconv.IntSize != 32 {
		t.Skip("int64 overflow is only representable when int is 32 bits")
	}
	cfg := newTestConfig(t, map[string]any{"value": int64(1 << 32)})
	_, err := cfg.GetInt("value")
	require.ErrorIs(t, err, ErrConfigInvalid)
}

func TestBuilderRegistersEveryHookKind(t *testing.T) {
	identity := hook.Func(func(_ context.Context, _ string, value any) (any, error) { return value, nil })
	condition := hook.Condition(func(_ context.Context, _ string, _ any) (bool, error) { return true, nil })

	cfg, err := NewBuilder[any]().
		AddLoader(&stubLoader{source: "builder-hooks", data: map[string]any{"key": "value"}}).
		WithKeyHook("key", identity).
		WithValueHook("value", identity).
		WithConditionHook(condition, identity).
		WithGlobalHook(identity).
		BuildWithContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value", cfg.GetOr("key", ""))

	_, err = NewBuilder[any]().
		AddLoader(&stubLoader{source: "builder-build", data: map[string]any{"key": "value"}}).
		Build()
	require.NoError(t, err)
}

func TestSelfConfigRejectsInvalidMergeAndTimeoutSettings(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "merge path", body: "merge:\n  paths:\n    database: invalid\n", want: "merge.paths"},
		{name: "startup timeout", body: "startup:\n  timeout: invalid\n", want: "startup.timeout"},
		{name: "runtime timeout", body: "runtime:\n  timeout: invalid\n", want: "runtime.timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(test.body), 0o600))
			selfconfig.ClearCache()
			t.Cleanup(selfconfig.ClearCache)
			_, err := New[any](WithWorkingDir(dir), WithLoaders())
			require.ErrorIs(t, err, ErrConfigLoad)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestMaterializationRejectsInvalidContextsAndLifecycle(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1, "b": map[string]any{"c": 2}})
	var nilContext context.Context
	require.ErrorIs(t, cfg.materializeEffectiveConfig(nilContext), ErrConfigInvalid)
	_, err := cfg.materializeEffectiveValue(nilContext, "key", "value")
	require.ErrorIs(t, err, ErrConfigInvalid)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, cfg.materializeEffectiveConfig(canceled), context.Canceled)
	_, err = cfg.materializeEffectiveValue(canceled, "key", "value")
	require.ErrorIs(t, err, context.Canceled)
	_, err = cfg.applySecretHookRecursiveMode(canceled, "", map[string]any{"key": "value"}, false)
	require.ErrorIs(t, err, context.Canceled)
	_, err = cfg.applySecretHookToSlice(canceled, "key", []any{"value"})
	require.ErrorIs(t, err, context.Canceled)

	require.ErrorIs(t, cfg.refreshSecretsAttempt(canceled, time.Now()), context.Canceled)
	cfg.closed = true
	require.ErrorIs(t, cfg.refreshSecretsAttempt(context.Background(), time.Now()), ErrConfigClosed)
	cfg.closed = false
	cfg.frozen = true
	require.ErrorIs(t, cfg.refreshSecretsAttempt(context.Background(), time.Now()), ErrConfigFrozen)
}

func TestParallelMaterializationCancelsOnHookFailure(t *testing.T) {
	boom := errors.New("provider failed")
	processor := hook.NewProcessor()
	processor.RegisterGlobalHook(func(_ context.Context, key string, value any) (any, error) {
		if key == "a" {
			return nil, boom
		}
		return value, nil
	})
	cfg := &Config[any]{
		opts: options{
			SecretResolutionConcurrency: 1,
		},
		hookProcessor: processor,
	}
	_, err := cfg.applySecretHookMapParallel(context.Background(), "", map[string]any{
		"a": "fail", "b": []any{"value"}, "c": map[string]any{"nested": "value"}, "d": "value",
	})
	require.ErrorIs(t, err, boom)
}

func TestRefreshSecretsObservesCancellationBeforePublication(t *testing.T) {
	var refresh atomic.Bool
	var cancel context.CancelFunc
	processor := hook.NewProcessor()
	processor.RegisterGlobalHook(func(_ context.Context, _ string, value any) (any, error) {
		if refresh.Load() {
			cancel()
		}
		return value, nil
	})
	cfg := &Config[any]{
		opts:                defaultOptions(),
		hookProcessor:       processor,
		envConfig:           map[string]any{"key": "ready"},
		unresolvedEnvConfig: map[string]any{"key": "ready"},
	}
	ctx, cancelOperation := context.WithCancel(context.Background())
	cancel = cancelOperation
	refresh.Store(true)
	require.ErrorIs(t, cfg.refreshSecretsAttempt(ctx, time.Now()), context.Canceled)
}

func TestObserveOperationsPropagateInvalidContexts(t *testing.T) {
	left := newTestConfig(t, map[string]any{"key": "left"})
	right := newTestConfig(t, map[string]any{"key": "right"})
	changes, err := left.Diff(right)
	require.NoError(t, err)
	require.NotEmpty(t, changes)

	var nilContext context.Context
	_, err = left.DiffWithContext(nilContext, right)
	require.ErrorIs(t, err, ErrConfigInvalid)
	_, err = left.DetectDriftWithContext(nilContext, nil)
	require.ErrorIs(t, err, ErrConfigInvalid)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = left.DiffWithContext(canceled, right)
	require.ErrorIs(t, err, context.Canceled)
	_, err = left.DetectDriftWithContext(canceled, nil)
	require.ErrorIs(t, err, context.Canceled)
	_, err = left.SaveVersionWithContext(canceled, nil)
	require.ErrorIs(t, err, context.Canceled)

	left.mu.Lock()
	left.envConfig = map[string]any{"unsupported": make(chan int)}
	left.mu.Unlock()
	_, err = left.SaveVersionWithContext(context.Background(), nil)
	require.Error(t, err)

	left = newTestConfig(t, map[string]any{"key": "left"})
	right = newTestConfig(t, map[string]any{"key": "right"})
	diffCtx, cancelDiff := context.WithCancel(context.Background())
	_, err = left.DiffWithContext(&cancelOnErrCheckContext{Context: diffCtx, cancel: cancelDiff, at: 2}, right)
	require.ErrorIs(t, err, context.Canceled)
	saveCtx, cancelSave := context.WithCancel(context.Background())
	_, err = left.SaveVersionWithContext(&cancelOnErrCheckContext{Context: saveCtx, cancel: cancelSave, at: 2}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCloseStopsDynamicWatchingAndCallbacksRecoverPanics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: value\n"), 0o600))
	cfg, err := New[any](
		WithLoaders(&fileAutoLoader{path: path}),
		WithDynamicReloading(true),
	)
	require.NoError(t, err)
	require.NoError(t, cfg.Close())

	callbacks := newTestConfig(t, map[string]any{"key": "old"})
	callbacks.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	callbacks.OnChange(nil)
	callbacks.OnChangeWithContext(nil)
	callbacks.OnChangeWithContext(func(context.Context, string, any, any) { panic("intentional callback panic") })
	require.NoError(t, callbacks.Set("key", "new"))
}

type constructionCloseableStore struct{ closed atomic.Int32 }

func (*constructionCloseableStore) ReadSecret(context.Context, SecretRequest) (any, error) {
	return "resolved", nil
}

func (s *constructionCloseableStore) Close() error { s.closed.Add(1); return nil }

func TestConstructorRejectsConcurrencyAndClosesProviderOnFailure(t *testing.T) {
	_, err := New[any](WithSecretResolutionConcurrency(0), WithLoaders())
	require.ErrorIs(t, err, ErrConfigLoad)

	store := &constructionCloseableStore{}
	providerType := "construction-cleanup-test"
	RegisterSelfConfigSecretProvider(providerType, func(context.Context, map[string]any) (SecretReader, error) {
		return store, nil
	})
	t.Cleanup(func() { selfConfigSecretProviders.Delete(providerType) })
	dir := t.TempDir()
	selfConfig := "secrets:\n  default_provider: test\n  providers:\n    test:\n      type: " + providerType + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(selfConfig), 0o600))
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)
	_, err = New[any](
		WithWorkingDir(dir),
		WithLoaders(&stubLoader{source: "cleanup", data: map[string]any{"secret": "${secret:key}"}}),
		WithValidator(errorValidator{err: errors.New("reject candidate")}),
	)
	require.Error(t, err)
	assert.Equal(t, int32(1), store.closed.Load())
}

type errorValidator struct{ err error }

func (v errorValidator) Validate(map[string]any) error { return v.err }

func TestSecretSelfConfigCollectorAndLegacyProviderGuards(t *testing.T) {
	var collector *selfConfigResourceCollector
	collector.add(nil)
	(&selfConfigResourceCollector{}).add(nil)

	_, _, _, err := buildNamedSelfConfigSecretHook(map[string]any{
		"providers": map[string]any{
			"legacy": map[string]any{"provider": "dict", "type": "dict"},
		},
	}, "")
	require.ErrorIs(t, err, ErrConfigLoad)

	_, err = extractSelfConfigSecretPath(map[string]any{"object": 42}, "object.child")
	require.ErrorIs(t, err, ErrSecretValidation)
}

func TestImplicitOperationContextWithoutTimeout(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"key": "value"}, WithOperationTimeout(0))
	ctx, cancel := cfg.implicitOperationContext()
	defer cancel()
	_, hasDeadline := ctx.Deadline()
	assert.False(t, hasDeadline)
}

type cancelingLoader struct {
	cancel context.CancelFunc
	data   map[string]any
}

func (l *cancelingLoader) Load(context.Context) (map[string]any, error) {
	l.cancel()
	return l.data, nil
}

func (*cancelingLoader) Source() string { return "canceling-loader" }

func TestLoadPipelineStopsAfterLoaderCancellation(t *testing.T) {
	t.Run("before loader", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		cfg := &Config[any]{
			opts:          defaultOptions(),
			loaders:       []Loader{&stubLoader{source: "must-not-run", data: map[string]any{"key": "value"}}},
			envHandler:    envhandler.New(nil),
			sourceTracker: sourcetrack.NewTracker(false),
			composer:      compose.New("."),
		}
		require.ErrorIs(t, cfg.loadSelected(ctx, nil), context.Canceled)
	})

	t.Run("between loaders", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		_, err := NewWithContext[any](ctx,
			WithStartupTimeout(0),
			WithLoaders(
				&cancelingLoader{cancel: cancel, data: map[string]any{"key": "value"}},
				&stubLoader{source: "must-not-run", data: map[string]any{"other": "value"}},
			),
		)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("during composition", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		_, err := NewWithContext[any](ctx,
			WithStartupTimeout(0),
			WithLoaders(&cancelingLoader{
				cancel: cancel,
				data:   map[string]any{"_include": "unused.yaml"},
			}),
		)
		require.ErrorIs(t, err, context.Canceled)
	})
}
