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

var secretPattern = regexp.MustCompile(`\$\{secret:([^}:]+)(?::([^}:]+))?(?::([^}]+))?\}`)

// Resolver bridges a SecretStore with the hook system, resolving
// ${secret:key}, ${secret:key:json_path}, and ${secret:key:json_path:version} placeholders.
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

// Resolve resolves all ${secret:...} placeholders in a string value.
func (r *Resolver) Resolve(ctx context.Context, value string) (string, error) {
	if !strings.Contains(value, "${secret:") {
		return value, nil
	}

	var lastErr error
	result := secretPattern.ReplaceAllStringFunc(value, func(match string) string {
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

	return result, lastErr
}

// Hook returns a hook.Func that can be registered on a HookProcessor.
func (r *Resolver) Hook() hook.Func {
	return func(_ string, value any) any {
		s, ok := value.(string)
		if !ok {
			return value
		}
		resolved, err := r.Resolve(context.Background(), s)
		if err != nil {
			return value // leave unchanged on error
		}
		return resolved
	}
}

// ClearCache clears the internal secret cache.
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]cacheEntry)
}

// CacheStats returns cache statistics.
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

// Prefetch pre-populates the cache for the given keys.
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
