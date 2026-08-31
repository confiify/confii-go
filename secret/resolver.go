// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/internal/secretref"
)

// secretPattern is the shared grammar from internal/secretref. The
// definition lives there because the root package needs it too and cannot
// import this one.
var secretPattern = secretref.Pattern()

// ErrProviderRoutingUnsupported reports a provider-qualified reference handed
// to a [Resolver], which holds a single store and performs no routing.
//
// A reference such as ${secret@vault:key} names the backend it must come from.
// A Resolver cannot honor that, and resolving against its only store would
// return a value from the wrong backend without saying so. Provider routing
// belongs to the configuration layer, which owns the provider registry:
// configure `secrets.providers` declaratively instead.
// It wraps [confii.ErrSecretValidation]: the reference is well formed but asks
// for something this resolver cannot provide, which is a validation failure of
// the input rather than a store or transport failure.
var ErrProviderRoutingUnsupported = fmt.Errorf(
	"%w: provider-qualified reference requires configuration-level routing",
	confii.ErrSecretValidation)

// Resolver bridges a SecretStore with the hook system, resolving
// ${secret:key}, ${secret:key:json_path}, ${secret:key:json_path:version}, and
// ${secret:key::version} placeholders. It is safe for concurrent use when its
// SecretStore is concurrency-safe. Cache entries may contain secret material
// and remain in process memory until expiration or ClearCache.
type Resolver struct {
	store        confii.SecretStore
	cacheEnabled bool
	cacheTTL     time.Duration
	prefix       string

	mu    sync.RWMutex
	cache map[string]cacheEntry

	// Lifecycle. lifeMu makes the closing check and the in-flight
	// registration atomic with respect to Close; see Resolver.enter.
	lifeMu         sync.Mutex
	closing        bool
	inFlight       sync.WaitGroup
	closeOnce      sync.Once
	closeErr       error
	closeCtx       context.Context
	cancelInFlight context.CancelFunc
}

type cacheEntry struct {
	value     any
	timestamp time.Time
}

// ResolverOption configures a Resolver.
type ResolverOption func(*Resolver)

// WithCache enables or disables successful-read caching. Caching is enabled by
// default. Disabling it does not remove existing entries; call
// [Resolver.ClearCache] when cached secret material must be discarded.
func WithCache(v bool) ResolverOption {
	return func(r *Resolver) { r.cacheEnabled = v }
}

// WithCacheTTL sets the cache time-to-live. Zero means entries do not expire
// automatically; negative durations cause every cached entry to be treated as
// expired.
func WithCacheTTL(d time.Duration) ResolverOption {
	return func(r *Resolver) { r.cacheTTL = d }
}

// WithResolverPrefix prepends p verbatim to every secret key before store
// lookup. The caller is responsible for separators such as "/".
func WithResolverPrefix(p string) ResolverOption {
	return func(r *Resolver) { r.prefix = p }
}

// NewResolver creates a resolver with caching enabled and no automatic cache
// expiry. store must be non-nil and must honor context cancellation. Options
// are applied in order, with later options taking precedence.
func NewResolver(store confii.SecretStore, opts ...ResolverOption) *Resolver {
	r := &Resolver{
		store:        store,
		cacheEnabled: true,
		cache:        make(map[string]cacheEntry),
	}
	r.closeCtx, r.cancelInFlight = context.WithCancel(context.Background())
	for _, o := range opts {
		o(r)
	}
	return r
}

// Resolve replaces all ${secret:...} placeholders in a string value with
// their resolved secret values.
//
// Resolution stops at the first failing placeholder and returns that error,
// leaving the input unchanged so a failed operation never publishes a
// partially resolved value.
func (r *Resolver) Resolve(ctx context.Context, value string) (string, error) {
	// Deliberately the literal unqualified prefix, not secretref.Contains.
	//
	// Widening this to recognize ${secret@provider:...} would make a value
	// that is only provider-qualified reach the scan below, and substitution
	// can synthesize a placeholder in its own output: resolving
	// ${secret@${secret:k}:f} yields ${secret@<value>:f}, which a second pass
	// would then try to resolve. That re-entrancy is a pre-existing defect in
	// the resolver, independent of this grammar, and widening the check here
	// would enlarge its surface. The guard below still catches a
	// provider-qualified reference sharing a value with an unqualified one,
	// which is the case that would otherwise resolve against the wrong store.
	if !strings.Contains(value, "${secret:") {
		return value, nil
	}

	var lastErr error
	result := secretPattern.ReplaceAllStringFunc(value, func(match string) string {
		// Short-circuit once any placeholder has
		// failed, the store must not be invoked for subsequent matches —
		// the caller will receive the original input verbatim alongside
		// the first error, so any further substitution work is wasted
		// and would mask the fail-fast contract.
		if lastErr != nil {
			return match
		}

		ref := secretref.FromMatch(secretPattern.FindStringSubmatch(match))
		if ref.Key == "" {
			return match
		}
		if ref.Provider != "" {
			// A Resolver has exactly one store and performs no routing.
			// Resolving against that store anyway would silently ignore the
			// provider the author named and return a value from the wrong
			// backend. Provider-qualified references belong to the
			// configuration layer, which owns the provider registry.
			lastErr = fmt.Errorf("%w: %s names provider %q",
				ErrProviderRoutingUnsupported, ref.Key, ref.Provider)
			return match
		}

		resolved, err := r.resolveKey(ctx, ref.Key, ref.Field, ref.Version)
		if err != nil {
			lastErr = err
			return match // leave placeholder unchanged
		}
		return fmt.Sprintf("%v", resolved)
	})

	if lastErr != nil {
		// Fail-fast contract: return the original input so callers can
		// trust that an error response never reflects a partially
		// resolved string (which could leak earlier successful values
		// while masking the true failure).
		return value, lastErr
	}

	// Every placeholder in the input has now been replaced, so any reference
	// still matching in the result was manufactured by the substitution
	// itself. Two ways that happens:
	//
	//   - a resolved value is, or contains, a reference; or
	//   - a resolved value ends in '$' and the literal text after it begins
	//     with '{', so the seam between them spells a new reference that
	//     appears in neither the value nor the template alone.
	//
	// Either way the result now carries a reference the author did not write,
	// and anything resolving it again would read a secret nobody asked for.
	// Reject rather than emit it. The error names no part of the result,
	// because the synthesized reference is built from resolved material.
	if secretPattern.MatchString(result) {
		return value, fmt.Errorf(
			"%w: resolving %s produced a value that spells a new secret reference; "+
				"a resolved secret must not introduce one",
			confii.ErrSecretValidation, describeResolvedKeys(value))
	}
	return result, nil
}

// describeResolvedKeys lists the locators a value asked for, so a synthesis
// failure can be traced without quoting anything that was resolved. Locators
// come from the input template and name where secrets live, never what they
// hold.
func describeResolvedKeys(input string) string {
	matches := secretPattern.FindAllStringSubmatch(input, -1)
	keys := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, groups := range matches {
		// Read through the shared accessor. Group 1 is the provider in the
		// unified grammar, not the key.
		key := secretref.FromMatch(groups).Key
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, strconv.Quote(key))
	}
	if len(keys) == 0 {
		return "the value"
	}
	return strings.Join(keys, ", ")
}

// Hook returns a context-aware [hook.Func] that resolves secret placeholders
// in string values. Store, cancellation, and missing-secret errors propagate
// to the configuration operation instead of being converted into unresolved
// placeholder strings.
func (r *Resolver) Hook() hook.Func {
	return func(ctx context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok {
			return value, nil
		}
		resolved, err := r.Resolve(ctx, s)
		if err != nil {
			return value, err
		}
		return resolved, nil
	}
}

// ClearCache removes all cached secret values. It is safe for concurrent use;
// an in-flight provider read may populate the cache after ClearCache returns.
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]cacheEntry)
}

// CacheStats returns a detached map with "enabled", "size", and "keys". Keys
// are backend key identifiers and may reveal sensitive naming information;
// ordering is unspecified.
func (r *Resolver) CacheStats() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.cache))
	for k := range r.cache {
		keys = append(keys, k)
	}
	return map[string]any{
		"enabled": r.cacheEnabled,
		"size":    len(r.cache),
		"keys":    keys,
	}
}

// Prefetch resolves keys in order using the current prefix and cache policy.
// It stops at the first context or provider error. Successfully resolved keys
// before that error remain cached; when caching is disabled Prefetch performs
// the reads but retains nothing.
func (r *Resolver) Prefetch(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := r.resolveKey(ctx, key, "", "")
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) resolveKey(ctx context.Context, key, jsonPath, version string) (any, error) {
	if ctx == nil {
		return nil, errors.New("secret resolver: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !r.enter() {
		return nil, ErrResolverClosed
	}
	defer r.leave()

	// Cancelled by the caller or by Close, so a parked provider read does not
	// outlive shutdown. Held for the cache write too, so the WaitGroup above
	// covers the whole critical section.
	ctx, cancel := r.resolverContext(ctx)
	defer cancel()

	fullKey := key
	if r.prefix != "" {
		fullKey = r.prefix + key
	}

	cacheKey := fullKey + ":" + version

	// Check cache.
	if r.cacheEnabled {
		r.mu.RLock()
		if entry, ok := r.cache[cacheKey]; ok {
			if r.cacheTTL == 0 || time.Since(entry.timestamp) < r.cacheTTL {
				r.mu.RUnlock()
				return r.extractPath(entry.value, jsonPath)
			}
		}
		r.mu.RUnlock()
	}

	// Fetch from store.
	var opts []confii.SecretOption
	if version != "" {
		opts = append(opts, confii.WithVersion(version))
	}
	val, err := r.store.GetSecret(ctx, fullKey, opts...)
	if err != nil {
		return nil, err
	}

	// Cache the result.
	if r.cacheEnabled {
		r.mu.Lock()
		r.cache[cacheKey] = cacheEntry{value: val, timestamp: time.Now()}
		r.mu.Unlock()
	}

	return r.extractPath(val, jsonPath)
}

func (r *Resolver) extractPath(val any, jsonPath string) (any, error) {
	if jsonPath == "" {
		return val, nil
	}

	// Traverse dot-separated path through nested maps.
	parts := strings.Split(jsonPath, ".")
	current := val
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: cannot traverse path %q in non-map value", confii.ErrSecretValidation, jsonPath)
		}
		current, ok = m[part]
		if !ok {
			return nil, fmt.Errorf("%w: path %q not found in secret", confii.ErrSecretValidation, jsonPath)
		}
	}
	return current, nil
}
