// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"fmt"
)

// ErrResolverClosed reports an operation attempted on a closed [Resolver].
// Resolve and Prefetch return it once [Resolver.Close] has begun.
var ErrResolverClosed = errors.New("secret resolver: closed")

// Close shuts the resolver down and releases what it owns. It is idempotent
// and safe to call concurrently: every caller observes the same result, and the
// underlying store is closed at most once.
//
// The sequence is:
//
//  1. New Resolve and Prefetch calls are rejected with [ErrResolverClosed].
//  2. In-flight resolutions are cancelled through a resolver-owned context.
//  3. Close waits for every in-flight resolution to finish, including its
//     cache write, so a late completion cannot repopulate the cache.
//  4. Resolver-owned cached values are dropped.
//  5. The store is closed when it implements Close() error, and any failure is
//     aggregated rather than returned in place of later cleanup.
//
// # Memory boundary
//
// Close bounds ownership and retention; it does not erase memory. Go offers no
// way to guarantee that every copy of a secret is overwritten: values may have
// been copied into caller structures, interned by the runtime, or left in
// garbage that has not yet been collected. What Close does guarantee is that
// the resolver holds no cached secret after it returns, performs no further
// reads, and hands out no further values. Treat resolved material already
// returned to the caller as the caller's to manage.
func (r *Resolver) Close() error {
	r.closeOnce.Do(func() {
		// Reject new work and stop new in-flight registrations under the same
		// lock the entry path uses, so the wait below cannot race an arrival.
		r.lifeMu.Lock()
		r.closing = true
		r.lifeMu.Unlock()

		// Unblock provider reads that are parked on the network.
		if r.cancelInFlight != nil {
			r.cancelInFlight()
		}

		// Every resolution holds the group for its whole duration, cache write
		// included, so this returns only once no writer remains.
		r.inFlight.Wait()

		r.mu.Lock()
		r.cache = make(map[string]cacheEntry)
		r.mu.Unlock()

		if closer, ok := r.store.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				r.closeErr = errors.Join(r.closeErr, fmt.Errorf("secret store: %w", err))
			}
		}
	})
	return r.closeErr
}

// enter registers a resolution attempt. It reports false once Close has begun,
// in which case the caller must not touch resolver state.
//
// Registration happens under lifeMu rather than through a bare atomic so that
// the closing check and the WaitGroup increment are one step relative to
// Close, which would otherwise be able to start waiting between them.
func (r *Resolver) enter() bool {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	if r.closing {
		return false
	}
	r.inFlight.Add(1)
	return true
}

func (r *Resolver) leave() { r.inFlight.Done() }

// resolverContext derives a context cancelled either by the caller or by
// Close, so a resolution parked on a provider read does not outlive shutdown.
func (r *Resolver) resolverContext(ctx context.Context) (context.Context, context.CancelFunc) {
	derived, cancel := context.WithCancel(ctx)
	if r.closeCtx == nil {
		return derived, cancel
	}
	stop := context.AfterFunc(r.closeCtx, cancel)
	return derived, func() {
		stop()
		cancel()
	}
}
