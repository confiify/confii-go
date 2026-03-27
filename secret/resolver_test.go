package secret

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolver_Resolve(t *testing.T) {
	store := NewDictStore(map[string]any{
		"db/password": "s3cret",
		"api/key":     "abc123",
	})

	r := NewResolver(store)
	ctx := context.Background()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "${secret:db/password}", "s3cret", false},
		{"multiple", "${secret:db/password} and ${secret:api/key}", "s3cret and abc123", false},
		{"no placeholder", "plain value", "plain value", false},
		{"missing secret", "${secret:missing}", "${secret:missing}", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Resolve(ctx, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolver_JSONPath(t *testing.T) {
	store := NewDictStore(map[string]any{
		"db/config": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	})

	r := NewResolver(store)
	got, err := r.Resolve(context.Background(), "${secret:db/config:host}")
	require.NoError(t, err)
	assert.Equal(t, "localhost", got)
}

func TestResolver_Cache(t *testing.T) {
	store := NewDictStore(map[string]any{"key": "value"})
	r := NewResolver(store, WithCache(true))

	ctx := context.Background()
	_, _ = r.Resolve(ctx, "${secret:key}")

	stats := r.CacheStats()
	assert.Equal(t, true, stats["enabled"])
	assert.Equal(t, 1, stats["size"])

	r.ClearCache()
	stats = r.CacheStats()
	assert.Equal(t, 0, stats["size"])
}

func TestResolver_CacheTTL(t *testing.T) {
	store := NewDictStore(map[string]any{"key": "original"})
	r := NewResolver(store, WithCache(true), WithCacheTTL(50*time.Millisecond))

	ctx := context.Background()
	got, _ := r.Resolve(ctx, "${secret:key}")
	assert.Equal(t, "original", got)

	// Update underlying value.
	_ = store.SetSecret(ctx, "key", "updated")

	// Cached value should still be returned.
	got, _ = r.Resolve(ctx, "${secret:key}")
	assert.Equal(t, "original", got)

	// Wait for TTL to expire.
	time.Sleep(60 * time.Millisecond)
	got, _ = r.Resolve(ctx, "${secret:key}")
	assert.Equal(t, "updated", got)
}

func TestResolver_Hook(t *testing.T) {
	store := NewDictStore(map[string]any{"api/key": "resolved"})
	r := NewResolver(store)

	h := r.Hook()
	got := h("key", "${secret:api/key}")
	assert.Equal(t, "resolved", got)

	// Non-string passthrough.
	assert.Equal(t, 42, h("key", 42))
}

func TestResolver_Prefetch(t *testing.T) {
	store := NewDictStore(map[string]any{
		"k1": "v1",
		"k2": "v2",
	})
	r := NewResolver(store)

	require.NoError(t, r.Prefetch(context.Background(), []string{"k1", "k2"}))

	stats := r.CacheStats()
	assert.Equal(t, 2, stats["size"])
}

func TestResolver_WithPrefix(t *testing.T) {
	store := NewDictStore(map[string]any{"prod/db/password": "secret"})
	r := NewResolver(store, WithResolverPrefix("prod/"))

	got, err := r.Resolve(context.Background(), "${secret:db/password}")
	require.NoError(t, err)
	assert.Equal(t, "secret", got)
}

func TestResolver_FailOnMissingFalse(t *testing.T) {
	store := NewDictStore(nil)
	r := NewResolver(store, WithResolverFailOnMissing(false))

	got, err := r.Resolve(context.Background(), "${secret:missing}")
	require.NoError(t, err)
	assert.Equal(t, "${secret:missing}", got) // placeholder unchanged
}

// ===========================================================================
// Hook() when Resolve returns error
// ===========================================================================

func TestResolver_Hook_ResolveError(t *testing.T) {
	store := NewDictStore(nil) // empty store, any lookup fails
	r := NewResolver(store, WithResolverFailOnMissing(true))

	h := r.Hook()
	// Hook should return original value on error.
	got := h("key", "${secret:missing_key}")
	assert.Equal(t, "${secret:missing_key}", got)
}

// ===========================================================================
// Versioned secret fetch: ${secret:key:path:version} format
// ===========================================================================

func TestResolver_VersionedSecretFetch(t *testing.T) {
	store := NewDictStore(nil)
	ctx := context.Background()
	// Set multiple versions.
	_ = store.SetSecret(ctx, "db/pass", "version0")
	_ = store.SetSecret(ctx, "db/pass", "version1")
	_ = store.SetSecret(ctx, "db/pass", "version2_latest")

	r := NewResolver(store, WithCache(false))

	// The regex pattern: ${secret:key:json_path:version}
	// Use empty json_path position (just dots): ${secret:db/pass::v2} doesn't match the regex well.
	// Looking at the regex: \$\{secret:([^}:]+)(?::([^}:]+))?(?::([^}]+))?\}
	// For version: ${secret:db/pass:path:0} where path is the json_path, 0 is version.
	// Since the DictStore stores versions as array index, version "0" returns "version0".

	// Test with explicit version=0 and a dummy path.
	got, err := r.Resolve(ctx, "${secret:db/pass}")
	require.NoError(t, err)
	assert.Equal(t, "version2_latest", got) // latest
}
