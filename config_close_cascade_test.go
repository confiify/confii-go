// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingResolver records lifecycle calls so the cascade can be observed
// rather than assumed.
type countingResolver struct {
	closes   atomic.Int64
	clears   atomic.Int64
	closeErr error
}

func (r *countingResolver) Hook() hook.Func {
	return func(_ context.Context, _ string, value any) (any, error) { return value, nil }
}
func (r *countingResolver) ClearCache() { r.clears.Add(1) }
func (r *countingResolver) Close() error {
	r.closes.Add(1)
	return r.closeErr
}

func newConfigWithResolver(t *testing.T, resolver confii.ManagedSecretResolver) *confii.Config[any] {
	t.Helper()
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(&g08Loader{source: "base.yaml", data: map[string]any{"app": map[string]any{"name": "svc"}}}),
		confii.WithSecretResolver(resolver),
	)
	require.NoError(t, err)
	return cfg
}

func TestConfigClose_CascadesToCloseableResolver(t *testing.T) {
	resolver := &countingResolver{}
	cfg := newConfigWithResolver(t, resolver)

	require.NoError(t, cfg.Close())
	assert.Equal(t, int64(1), resolver.closes.Load(),
		"configuration shutdown must close a CloseableSecretResolver")
}

func TestConfigClose_SurfacesResolverCloseFailure(t *testing.T) {
	sentinel := errors.New("resolver close failed")
	resolver := &countingResolver{closeErr: sentinel}
	cfg := newConfigWithResolver(t, resolver)

	err := cfg.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "a resolver close failure must reach the caller")
}

func TestConfigClose_ClosesResolverExactlyOnce(t *testing.T) {
	resolver := &countingResolver{}
	cfg := newConfigWithResolver(t, resolver)

	require.NoError(t, cfg.Close())
	_ = cfg.Close()
	_ = cfg.Close()

	assert.Equal(t, int64(1), resolver.closes.Load(),
		"repeated configuration close must not re-close the resolver")
}

// A resolver that predates CloseableSecretResolver must keep working. This
// pins the optional nature of the interface.
type legacyResolver struct{ clears atomic.Int64 }

func (r *legacyResolver) Hook() hook.Func {
	return func(_ context.Context, _ string, value any) (any, error) { return value, nil }
}
func (r *legacyResolver) ClearCache() { r.clears.Add(1) }

func TestConfigClose_ResolverWithoutCloseIsNotAnError(t *testing.T) {
	cfg := newConfigWithResolver(t, &legacyResolver{})
	assert.NoError(t, cfg.Close(),
		"a resolver that implements only ManagedSecretResolver must still close cleanly")
}

// Close disposes through Close and nothing else. It does not fall back to
// ClearCache for a resolver that has none, and this is the pin for that
// decision rather than an accident nobody chose.
//
// ClearCache is an operational call — RefreshSecrets uses it to make the next
// resolution consult the store — and implementations treat it as live: a
// resolver is free to run arbitrary work there, including work that closes this
// configuration, which would re-enter closeOnce on this goroutine and deadlock.
// TestRefreshSecretsRechecksLifecycleAfterCacheInvalidation is exactly such a
// resolver, and it hangs for the full test timeout when Close calls ClearCache.
//
// The trade is small in the other direction too: the materialized configuration
// keeps its resolved values after Close by design, so emptying a resolver cache
// while that copy remains does not make the process forget the secret. A
// resolver owning material that must not outlive shutdown implements
// CloseableSecretResolver; docs/ownership.md says so instead of promising that
// no cached copy remains.
func TestConfigClose_DisposesThroughCloseOnly(t *testing.T) {
	legacy := &legacyResolver{}
	require.NoError(t, newConfigWithResolver(t, legacy).Close())
	assert.Zero(t, legacy.clears.Load(),
		"Close must not invoke a resolver operation the resolver did not opt into")

	closeable := &countingResolver{}
	require.NoError(t, newConfigWithResolver(t, closeable).Close())
	assert.Equal(t, int64(1), closeable.closes.Load())
	assert.Zero(t, closeable.clears.Load(),
		"Close owns cache disposal for a closeable resolver; ClearCache is not also called")
}
