// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingStore lets a test hold a provider read open, so close can be
// exercised against genuinely in-flight work rather than a simulation of it.
type blockingStore struct {
	release   chan struct{}
	entered   chan struct{}
	reads     atomic.Int64
	closes    atomic.Int64
	closeErr  error
	failReads error
}

func newBlockingStore() *blockingStore {
	return &blockingStore{
		release: make(chan struct{}),
		entered: make(chan struct{}, 16),
	}
}

func (s *blockingStore) GetSecret(ctx context.Context, key string, _ ...confii.SecretOption) (any, error) {
	s.reads.Add(1)
	select {
	case s.entered <- struct{}{}:
	default:
	}
	if s.failReads != nil {
		return nil, s.failReads
	}
	select {
	case <-s.release:
		return "value-of-" + key, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *blockingStore) SetSecret(context.Context, string, any, ...confii.SecretOption) error {
	return nil
}
func (s *blockingStore) DeleteSecret(context.Context, string, ...confii.SecretOption) error {
	return nil
}
func (s *blockingStore) ListSecrets(context.Context, string) ([]string, error) { return nil, nil }

func (s *blockingStore) Close() error {
	s.closes.Add(1)
	return s.closeErr
}

// openStore resolves immediately.
type openStore struct{ blockingStore }

func newOpenStore() *openStore {
	s := &openStore{blockingStore: *newBlockingStore()}
	close(s.release)
	return s
}

func TestResolverClose_BeforeFirstUse(t *testing.T) {
	r := NewResolver(newOpenStore())
	require.NoError(t, r.Close())
	assert.Empty(t, r.CacheStats()["keys"])
}

func TestResolverClose_AfterSuccessfulResolution(t *testing.T) {
	store := newOpenStore()
	r := NewResolver(store)

	got, err := r.Resolve(context.Background(), "${secret:db/password}")
	require.NoError(t, err)
	require.Equal(t, "value-of-db/password", got)

	require.NoError(t, r.Close())
	assert.Empty(t, r.CacheStats()["keys"], "cache must be empty after close")
	assert.Equal(t, int64(1), store.closes.Load(), "a closeable store must be closed")
}

func TestResolverClose_RejectsSubsequentResolve(t *testing.T) {
	r := NewResolver(newOpenStore())
	require.NoError(t, r.Close())

	_, err := r.Resolve(context.Background(), "${secret:db/password}")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResolverClosed)
}

func TestResolverClose_RejectsPrefetchAfterClose(t *testing.T) {
	r := NewResolver(newOpenStore())
	require.NoError(t, r.Close())

	err := r.Prefetch(context.Background(), []string{"db/password"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrResolverClosed)
}

func TestResolverClose_CancelsInFlightResolution(t *testing.T) {
	store := newBlockingStore() // never released
	r := NewResolver(store)

	errCh := make(chan error, 1)
	go func() {
		_, err := r.Resolve(context.Background(), "${secret:db/password}")
		errCh <- err
	}()

	<-store.entered // the provider read is genuinely in flight
	require.NoError(t, r.Close())

	select {
	case err := <-errCh:
		require.Error(t, err, "close must unblock the in-flight resolution")
	case <-time.After(5 * time.Second):
		t.Fatal("close did not cancel the in-flight resolution")
	}
}

// The discriminating test for the whole change: a resolution that completes
// after close begins must not leave its value in the cache.
func TestResolverClose_InFlightWorkCannotRepopulateCache(t *testing.T) {
	store := newBlockingStore()
	r := NewResolver(store)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.Resolve(context.Background(), "${secret:db/password}")
	}()
	<-store.entered

	closeDone := make(chan error, 1)
	go func() { closeDone <- r.Close() }()

	// Let the provider succeed while close is in progress. Its cache write
	// races the teardown; the cache must still end up empty.
	time.Sleep(20 * time.Millisecond)
	close(store.release)

	select {
	case err := <-closeDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("close did not complete")
	}
	<-done

	assert.Empty(t, r.CacheStats()["keys"],
		"a resolution completing during close must not repopulate the cache")
}

func TestResolverClose_IsIdempotent(t *testing.T) {
	store := newOpenStore()
	r := NewResolver(store)

	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
	require.NoError(t, r.Close())

	assert.Equal(t, int64(1), store.closes.Load(), "the store must be closed exactly once")
}

func TestResolverClose_ConcurrentCloseIsSafe(t *testing.T) {
	store := newOpenStore()
	r := NewResolver(store)

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = r.Close() }()
	}
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err, "every concurrent Close must observe the same result")
	}
	assert.Equal(t, int64(1), store.closes.Load())
}

func TestResolverClose_ConcurrentResolveAndClose(t *testing.T) {
	store := newOpenStore()
	r := NewResolver(store)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Either resolves or reports the resolver closed; never panics
			// and never leaves state behind.
			_, err := r.Resolve(context.Background(), "${secret:db/password}")
			if err != nil {
				assert.ErrorIs(t, err, ErrResolverClosed)
			}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); _ = r.Close() }()
	wg.Wait()

	assert.Empty(t, r.CacheStats()["keys"], "cache must be empty once close returns")
}

func TestResolverClose_AggregatesStoreCloseFailure(t *testing.T) {
	sentinel := errors.New("store close failed")
	store := newOpenStore()
	store.closeErr = sentinel
	r := NewResolver(store)

	err := r.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "a store close failure must surface")
}

func TestResolverClose_StoreWithoutCloseIsFine(t *testing.T) {
	r := NewResolver(NewDictStore(map[string]any{"db/password": "hunter2"}))
	require.NoError(t, r.Close(), "a store that cannot be closed is not an error")
}

func TestResolverClose_ClearCacheStillWorksBeforeClose(t *testing.T) {
	store := newOpenStore()
	r := NewResolver(store)

	_, err := r.Resolve(context.Background(), "${secret:db/password}")
	require.NoError(t, err)
	require.NotEmpty(t, r.CacheStats()["keys"])

	r.ClearCache()
	assert.Empty(t, r.CacheStats()["keys"])
	require.NoError(t, r.Close())
}

func TestResolver_SatisfiesCloseableSecretResolver(t *testing.T) {
	var r any = NewResolver(newOpenStore())
	_, ok := r.(confii.CloseableSecretResolver)
	assert.True(t, ok, "Resolver must satisfy confii.CloseableSecretResolver")
}
