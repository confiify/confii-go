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
