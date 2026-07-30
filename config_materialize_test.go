// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/secret"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eagerMaterializeLoader struct {
	mu   sync.RWMutex
	data map[string]any
}

func (l *eagerMaterializeLoader) Source() string { return "eager-materialize-test" }
func (l *eagerMaterializeLoader) Load(context.Context) (map[string]any, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return cloneEagerMap(l.data), nil
}
func (l *eagerMaterializeLoader) replace(data map[string]any) {
	l.mu.Lock()
	l.data = data
	l.mu.Unlock()
}

func cloneEagerMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			result[key] = cloneEagerMap(typed)
		default:
			result[key] = typed
		}
	}
	return result
}

func TestNewEagerlyMaterializesSecretsAndReadsStayInMemory(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{
		"database": map[string]any{"password": "${secret:database}"},
	}}
	var mu sync.Mutex
	resolveCalls := 0
	h := func(_ context.Context, _ string, value any) (any, error) {
		text, ok := value.(string)
		if !ok || !strings.Contains(text, "${secret:") {
			return value, nil
		}
		mu.Lock()
		resolveCalls++
		mu.Unlock()
		return "startup-secret", nil
	}

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretHook(h),
	)
	require.NoError(t, err)
	mu.Lock()
	assert.Equal(t, 1, resolveCalls, "New must resolve the effective secret")
	mu.Unlock()

	for range 3 {
		value, getErr := cfg.Get("database.password")
		require.NoError(t, getErr)
		assert.Equal(t, "startup-secret", value)
	}
	_, err = cfg.ToDictWithContext(context.Background())
	require.NoError(t, err)
	mu.Lock()
	assert.Equal(t, 1, resolveCalls, "ordinary reads must not fetch providers again")
	mu.Unlock()
	assert.Equal(t, []string{"database.password"}, cfg.SecretReferenceKeys())
}

func TestNewFailsWhenEffectiveSecretCannotBeResolved(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{"token": "${secret:missing}"}}
	_, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretHook(func(context.Context, string, any) (any, error) {
			return nil, errors.New("provider unavailable")
		}),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
	assert.NotContains(t, err.Error(), "${secret:missing}")
}

func TestOnlySelectedEnvironmentIsEagerlyResolved(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{
		"default":     map[string]any{"name": "demo"},
		"development": map[string]any{"token": "${secret:development}"},
		"production":  map[string]any{"token": "${secret:production}"},
	}}
	var resolved []string
	h := func(_ context.Context, _ string, value any) (any, error) {
		text, ok := value.(string)
		if !ok || !strings.Contains(text, "${secret:") {
			return value, nil
		}
		resolved = append(resolved, text)
		if strings.Contains(text, "production") {
			return nil, errors.New("inactive production provider must not be contacted")
		}
		return "development-value", nil
	}
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnv("development"),
		confii.WithLoaders(loader),
		confii.WithSecretHook(h),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"${secret:development}"}, resolved)
	assert.Equal(t, "development-value", cfg.GetStringOr("token", ""))
}

func TestRefreshSecretsAtomicallyPublishesNewValues(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{"token": "${secret:rotating}"}}
	var mu sync.RWMutex
	current := "first"
	h := hook.Func(func(_ context.Context, _ string, value any) (any, error) {
		text, ok := value.(string)
		if !ok || !strings.Contains(text, "${secret:") {
			return value, nil
		}
		mu.RLock()
		defer mu.RUnlock()
		return current, nil
	})
	cfg, err := confii.NewWithContext[any](context.Background(), confii.WithLoaders(loader), confii.WithSecretHook(h))
	require.NoError(t, err)
	assert.Equal(t, "first", cfg.GetStringOr("token", ""))

	mu.Lock()
	current = "second"
	mu.Unlock()
	require.NoError(t, cfg.RefreshSecretsWithContext(context.Background()))
	assert.Equal(t, "second", cfg.GetStringOr("token", ""))
}

func TestEagerMaterializationTraversesNestedSlices(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{
		"services": []any{
			map[string]any{"credentials": []any{"${secret:first}", []any{"${secret:second}"}}},
		},
	}}
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretHook(func(_ context.Context, _ string, value any) (any, error) {
			text, ok := value.(string)
			if !ok || !strings.HasPrefix(text, "${secret:") {
				return value, nil
			}
			return "resolved:" + text, nil
		}),
	)
	require.NoError(t, err)
	value, err := cfg.Get("services")
	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"credentials": []any{
			"resolved:${secret:first}", []any{"resolved:${secret:second}"},
		}},
	}, value)
}

func TestRefreshSecretsFailurePreservesReadySnapshot(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{"token": "${secret:rotating}"}}
	var fail atomic.Bool
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretHook(func(_ context.Context, _ string, value any) (any, error) {
			text, isSecret := value.(string)
			if !isSecret || !strings.HasPrefix(text, "${secret:") {
				return value, nil
			}
			if fail.Load() {
				return nil, errors.New("provider unavailable")
			}
			return "known-good", nil
		}),
	)
	require.NoError(t, err)
	fail.Store(true)
	err = cfg.RefreshSecretsWithContext(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
	assert.Equal(t, "known-good", cfg.GetStringOr("token", ""))
}

func TestRefreshSecretsRejectsConcurrentRawMutation(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{"token": "${secret:rotating}"}}
	started := make(chan struct{})
	release := make(chan struct{})
	var refresh atomic.Bool
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretHook(func(_ context.Context, _ string, value any) (any, error) {
			text, isSecret := value.(string)
			if !isSecret || !strings.HasPrefix(text, "${secret:") {
				return value, nil
			}
			if refresh.Load() {
				close(started)
				<-release
			}
			return "resolved", nil
		}),
	)
	require.NoError(t, err)

	refresh.Store(true)
	done := make(chan error, 1)
	go func() { done <- cfg.RefreshSecretsWithContext(context.Background()) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh hook did not start")
	}
	require.NoError(t, cfg.Set("concurrent", true))
	close(release)
	err = <-done
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
	assert.True(t, cfg.GetBoolOr("concurrent", false))
}

func TestRefreshSecretsEmitsChangesAndMetrics(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{"token": "${secret:rotating}"}}
	var current atomic.Value
	current.Store("first")
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretHook(func(_ context.Context, _ string, _ any) (any, error) {
			return current.Load().(string), nil
		}),
	)
	require.NoError(t, err)

	changed := make(chan struct{}, 1)
	refreshed := make(chan struct{}, 1)
	cfg.OnChange(func(key string, oldValue, newValue any) {
		if key == "token" && oldValue == "first" && newValue == "second" {
			changed <- struct{}{}
		}
	})
	cfg.EnableObservability()
	cfg.EnableEvents().On("secrets_refreshed", func(...any) { refreshed <- struct{}{} })
	current.Store("second")
	require.NoError(t, cfg.RefreshSecretsWithContext(context.Background()))
	select {
	case <-changed:
	default:
		t.Fatal("OnChange was not called for the refreshed value")
	}
	select {
	case <-refreshed:
	default:
		t.Fatal("secrets_refreshed event was not emitted")
	}
	metrics := cfg.GetMetrics()
	assert.EqualValues(t, 1, metrics["change_count"])
}

func TestRefreshSecretsValidationFailurePreservesReadySnapshot(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{"port": "${secret:port}"}}
	var current atomic.Int64
	current.Store(8080)
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader),
		confii.WithSecretHook(func(_ context.Context, _ string, value any) (any, error) {
			text, isSecret := value.(string)
			if !isSecret || !strings.HasPrefix(text, "${secret:") {
				return value, nil
			}
			return int(current.Load()), nil
		}),
		confii.WithSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"port": map[string]any{"type": "integer", "minimum": 1},
			},
			"required": []any{"port"},
		}),
		confii.WithValidateOnLoad(true),
	)
	require.NoError(t, err)
	current.Store(0)
	err = cfg.RefreshSecretsWithContext(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigValidation)
	assert.Equal(t, 8080, cfg.GetIntOr("port", 0))
}

func TestRefreshSecretsOnZeroConfigIsNoOp(t *testing.T) {
	t.Parallel()
	cfg := new(confii.Config[any])
	require.NoError(t, cfg.RefreshSecretsWithContext(context.Background()))
}

func TestManagedResolverRefreshBypassesItsCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := secret.NewDictStore(map[string]any{"rotating": "first"})
	resolver := secret.NewResolver(store, secret.WithCache(true))
	loader := &eagerMaterializeLoader{data: map[string]any{"token": "${secret:rotating}"}}
	cfg, err := confii.NewWithContext[any](ctx,
		confii.WithLoaders(loader),
		confii.WithSecretResolver(resolver),
	)
	require.NoError(t, err)
	assert.Equal(t, "first", cfg.GetStringOr("token", ""))

	require.NoError(t, store.SetSecret(ctx, "rotating", "second"))
	assert.Equal(t, "first", cfg.GetStringOr("token", ""), "ordinary reads retain the ready snapshot")
	require.NoError(t, cfg.RefreshSecretsWithContext(ctx))
	assert.Equal(t, "second", cfg.GetStringOr("token", ""), "refresh must invalidate the managed resolver cache")
}

func TestReloadRollsBackWhenEagerResolutionFails(t *testing.T) {
	t.Parallel()
	loader := &eagerMaterializeLoader{data: map[string]any{"token": "${secret:good}"}}
	h := func(_ context.Context, _ string, value any) (any, error) {
		text, ok := value.(string)
		if !ok || !strings.Contains(text, "${secret:") {
			return value, nil
		}
		if strings.Contains(text, "bad") {
			return nil, errors.New("secret unavailable")
		}
		return "known-good", nil
	}
	cfg, err := confii.NewWithContext[any](context.Background(), confii.WithLoaders(loader), confii.WithSecretHook(h))
	require.NoError(t, err)
	loader.replace(map[string]any{"token": "${secret:bad}"})

	err = cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false))
	require.Error(t, err)
	assert.Equal(t, "known-good", cfg.GetStringOr("token", ""))
	assert.Equal(t, []string{"token"}, cfg.SecretReferenceKeys())
}
