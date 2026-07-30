// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextMapLoader struct {
	name string
	data map[string]any
}

func (l *contextMapLoader) Load(context.Context) (map[string]any, error) { return copyMap(l.data), nil }
func (l *contextMapLoader) Name() string                                 { return l.name }
func (l *contextMapLoader) Source() string                               { return l.name }

type blockingReloadLoader struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (l *blockingReloadLoader) Load(ctx context.Context) (map[string]any, error) {
	if l.calls.Add(1) == 1 {
		return map[string]any{"value": "old"}, nil
	}
	select {
	case <-l.started:
	default:
		close(l.started)
	}
	select {
	case <-l.release:
		return map[string]any{"value": "new"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (*blockingReloadLoader) Name() string   { return "blocking-reload" }
func (*blockingReloadLoader) Source() string { return "remote:blocking" }

func TestReloadRemoteIODoesNotBlockReaders(t *testing.T) {
	loader := &blockingReloadLoader{started: make(chan struct{}), release: make(chan struct{})}
	cfg, err := New[any](WithLoaders(loader))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- cfg.ReloadWithContext(context.Background(), WithIncremental(false)) }()
	<-loader.started

	read := make(chan any, 1)
	go func() { value, _ := cfg.Get("value"); read <- value }()
	select {
	case value := <-read:
		assert.Equal(t, "old", value)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("reader blocked behind reload I/O")
	}
	close(loader.release)
	require.NoError(t, <-done)
	value, err := cfg.Get("value")
	require.NoError(t, err)
	assert.Equal(t, "new", value)
}

func TestEagerSecretMaterializationHonorsConcurrencyBound(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	hook := func(ctx context.Context, _ string, value any) (any, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		select {
		case <-time.After(20 * time.Millisecond):
			return value, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	cfg, err := New[any](WithLoaders(&contextMapLoader{name: "parallel", data: map[string]any{"a": 1, "b": 2, "c": 3, "d": 4}}),
		WithSecretHook(hook),
		WithSecretResolutionConcurrency(2),
	)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, int32(2), maximum.Load())
}

func TestOnChangeWithContextReceivesMutationContext(t *testing.T) {
	cfg, err := New[any](WithLoaders(&contextMapLoader{name: "context", data: map[string]any{"key": "old"}}))
	require.NoError(t, err)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "trace-123")
	var mu sync.Mutex
	var received any
	cfg.OnChangeWithContext(func(callbackCtx context.Context, key string, _, _ any) {
		if key == "key" {
			mu.Lock()
			received = callbackCtx.Value(contextKey{})
			mu.Unlock()
		}
	})
	require.NoError(t, cfg.SetWithContext(ctx, "key", "new"))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "trace-123", received)
}

type closeableLoader struct {
	closed atomic.Int32
}

type closeableSecretStore struct {
	closed atomic.Int32
}

func (*closeableSecretStore) ReadSecret(context.Context, SecretRequest) (any, error) {
	return "resolved", nil
}
func (s *closeableSecretStore) Close() error { s.closed.Add(1); return nil }

func (*closeableLoader) Load(context.Context) (map[string]any, error) { return map[string]any{}, nil }
func (*closeableLoader) Name() string                                 { return "closeable" }
func (*closeableLoader) Source() string                               { return "closeable" }
func (l *closeableLoader) Close() error                               { l.closed.Add(1); return nil }

func TestCloseReleasesOwnedLoaderExactlyOnce(t *testing.T) {
	loader := &closeableLoader{}
	cfg, err := New[any](WithLoaders(loader))
	require.NoError(t, err)
	require.NoError(t, cfg.Close())
	require.NoError(t, cfg.Close())
	assert.Equal(t, int32(1), loader.closed.Load())
}

func TestCloseReleasesDeclarativeProvider(t *testing.T) {
	providerType := "closeable-context-provider"
	store := &closeableSecretStore{}
	RegisterSelfConfigSecretProvider(providerType, func(context.Context, map[string]any) (SecretReader, error) {
		return store, nil
	})
	t.Cleanup(func() { selfConfigSecretProviders.Delete(providerType) })
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte("secrets:\n  default_provider: test\n  providers:\n    test:\n      type: "+providerType+"\n"), 0o600))
	cfg, err := New[any](WithWorkingDir(dir),
		WithLoaders(&contextMapLoader{name: "secret", data: map[string]any{"key": "${secret:key}"}}),
	)
	require.NoError(t, err)
	require.NoError(t, cfg.Close())
	assert.Equal(t, int32(1), store.closed.Load())
}
