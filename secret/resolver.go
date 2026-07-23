package secret

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/hook"
)

// secretPattern matches the supported secret placeholder grammar:
//
//	${secret:key}                    -> key only, current version
//	${secret:key:json_path}          -> key + json_path, current version
//	${secret:key:json_path:version}  -> key + json_path + explicit version
//	${secret:key::version}           -> key + empty json_path + explicit version
//
// Capture groups (in order): name, optional json_path, optional version.
// The json_path group accepts an empty match (zero or more) so the
// ${secret:key::version} form is recognized; the version group requires
// at least one character so a trailing colon alone (e.g. ${secret:key:}) is
// not interpreted as a version request. None of the components may contain
// colons or the closing brace.
var secretPattern = regexp.MustCompile(`\$\{secret:([^}:]+)(?::([^}:]*))?(?::([^}:]+))?\}`)

// Resolver bridges a SecretStore with the hook system, resolving
// ${secret:key}, ${secret:key:json_path}, ${secret:key:json_path:version}, and
// ${secret:key::version} placeholders.
type Resolver struct {
	store         confii.SecretStore
	cacheEnabled  bool
	cacheTTL      time.Duration
	failOnMissing bool
	prefix        string

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	value     any
	timestamp time.Time
}

// ResolverOption configures a Resolver.
type ResolverOption func(*Resolver)

// WithCache enables or disables caching.
func WithCache(v bool) ResolverOption {
	return func(r *Resolver) { r.cacheEnabled = v }
}

// WithCacheTTL sets the cache time-to-live. Zero means no expiration.
func WithCacheTTL(d time.Duration) ResolverOption {
	return func(r *Resolver) { r.cacheTTL = d }
}

// WithResolverFailOnMissing controls whether unresolvable secrets cause errors.
//
// When true (the default), [Resolver.Resolve] returns the underlying store
// error (wrapping [confii.ErrSecretNotFound] for missing keys) and the
// context-aware [Resolver.HookCtx] propagates that error to the caller of
// [Config.GetCtx]. Under this mode, resolution stops at the first failing
// placeholder and returns that error verbatim, leaving the input unchanged
// (no partial substitution of earlier successful placeholders is
// observable). When false, the placeholder is left in place, no error is
// raised, and the loop continues to substitute remaining placeholders —
// useful for soft fallbacks or staged rollouts.
//
// Note: the legacy context-free [Resolver.Hook] cannot surface errors due
// to its signature; to make this option effective end-to-end, register
// [Resolver.HookCtx] via [hook.Processor.RegisterGlobalHookCtx] and read
// values with [Config.GetCtx].
func WithResolverFailOnMissing(v bool) ResolverOption {
	return func(r *Resolver) { r.failOnMissing = v }
}

// WithResolverPrefix prepends a prefix to all secret keys.
func WithResolverPrefix(p string) ResolverOption {
	return func(r *Resolver) { r.prefix = p }
}

// NewResolver creates a new secret resolver.
func NewResolver(store confii.SecretStore, opts ...ResolverOption) *Resolver {
	r := &Resolver{
		store:         store,
		cacheEnabled:  true,
		failOnMissing: true,
		cache:         make(map[string]cacheEntry),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Resolve replaces all ${secret:...} placeholders in a string value with
// their resolved secret values.
//
// Under [WithResolverFailOnMissing](true), resolution stops at the first
// failing placeholder and returns that error verbatim, leaving the input
// unchanged (no partial substitution of earlier successful placeholders
// is observable to the caller). Under [WithResolverFailOnMissing](false)
// (legacy soft mode), failed placeholders are left in place and the loop
// continues to substitute the remaining placeholders; the function then
// returns the partially-substituted string with a nil error.
func (r *Resolver) Resolve(ctx context.Context, value string) (string, error) {
	if !strings.Contains(value, "${secret:") {
		return value, nil
	}

	var lastErr error
	result := secretPattern.ReplaceAllStringFunc(value, func(match string) string {
		// Short-circuit: under failOnMissing, once any placeholder has
		// failed we must not invoke the store for subsequent matches —
		// the caller will receive the original input verbatim alongside
		// the first error, so any further substitution work is wasted
		// and would mask the fail-fast contract.
		if lastErr != nil && r.failOnMissing {
			return match
		}

		groups := secretPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}

		key := groups[1]
		jsonPath := ""
		version := ""
		if len(groups) >= 3 {
			jsonPath = groups[2]
		}
		if len(groups) >= 4 {
			version = groups[3]
		}

		resolved, err := r.resolveKey(ctx, key, jsonPath, version)
		if err != nil {
			if r.failOnMissing {
				lastErr = err
			}
			return match // leave placeholder unchanged
		}
		return fmt.Sprintf("%v", resolved)
	})

	if lastErr != nil && r.failOnMissing {
		// Fail-fast contract: return the original input so callers can
		// trust that an error response never reflects a partially
		// resolved string (which could leak earlier successful values
		// while masking the true failure).
		return value, lastErr
	}
	return result, lastErr
}

// Hook returns a [hook.Func] that resolves secret placeholders in string
// values during hook processing.
//
// Because [hook.Func] is the legacy, context-free signature it cannot
// propagate the caller's context to the underlying [confii.SecretStore] and
// cannot surface resolution errors to the caller — even when the resolver
// was configured with [WithResolverFailOnMissing](true). The resolver
// invokes Resolve with [context.Background] and, on error, returns the
// original (unresolved) string. New code should register [Resolver.HookCtx]
// via [hook.Processor.RegisterGlobalHookCtx] (or sibling RegisterCtx
// methods) so that per-request context is honored and resolution errors
// reach the caller of [Config.GetCtx].
func (r *Resolver) Hook() hook.Func {
	return func(_ string, value any) any {
		s, ok := value.(string)
		if !ok {
			return value
		}
		resolved, err := r.Resolve(context.Background(), s)
		if err != nil {
			return value // leave unchanged on error (legacy behavior)
		}
		return resolved
	}
}

// HookCtx returns a [hook.FuncCtx] that resolves secret placeholders in
// string values during hook processing using the caller-supplied context.
//
// The returned hook honors the context's deadline, cancellation, and
// values (passing them through to [confii.SecretStore.GetSecret]). When
// the resolver is configured with [WithResolverFailOnMissing](true), an
// unresolvable secret causes the hook to return the resolution error
// (wrapping [confii.ErrSecretNotFound] or a store-specific error) instead
// of the original placeholder; the error then propagates up through
// [hook.Processor.ProcessCtx] to [Config.GetCtx]. With
// [WithResolverFailOnMissing](false) (default), the legacy behavior is
// preserved: the original placeholder is returned and no error is raised.
func (r *Resolver) HookCtx() hook.FuncCtx {
	return func(ctx context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok {
			return value, nil
		}
		resolved, err := r.Resolve(ctx, s)
		if err != nil {
			// Resolve only returns a non-nil error when failOnMissing is
			// true; surface it so the caller of Config.GetCtx fails fast
			// rather than receiving the unresolved placeholder.
			return value, err
		}
		return resolved, nil
	}
}

// ClearCache removes all entries from the internal secret cache.
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]cacheEntry)
}

// CacheStats returns a map containing cache statistics including enabled state, size, and cached keys.
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

// Prefetch pre-populates the cache by resolving the given keys from the underlying store.
func (r *Resolver) Prefetch(ctx context.Context, keys []string) error {
	for _, key := range keys {
		_, err := r.resolveKey(ctx, key, "", "")
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) resolveKey(ctx context.Context, key, jsonPath, version string) (any, error) {
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
