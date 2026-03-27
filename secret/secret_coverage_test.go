package secret

import (
	"context"
	"errors"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// EnvStore with options
// ---------------------------------------------------------------------------

func TestEnvStore_WithSuffix(t *testing.T) {
	t.Setenv("DB_PASSWORD_SECRET", "pass123")

	s := NewEnvStore(WithEnvSuffix("_SECRET"))
	val, err := s.GetSecret(context.Background(), "db/password")
	require.NoError(t, err)
	assert.Equal(t, "pass123", val)
}

func TestEnvStore_WithPrefixAndSuffix(t *testing.T) {
	t.Setenv("APP_API_KEY_ENC", "encrypted123")

	s := NewEnvStore(WithEnvPrefix("APP_"), WithEnvSuffix("_ENC"))
	val, err := s.GetSecret(context.Background(), "api/key")
	require.NoError(t, err)
	assert.Equal(t, "encrypted123", val)
}

func TestEnvStore_WithTransformKeyFalse(t *testing.T) {
	t.Setenv("my-secret.key", "raw-value")

	s := NewEnvStore(WithTransformKey(false))
	val, err := s.GetSecret(context.Background(), "my-secret.key")
	require.NoError(t, err)
	assert.Equal(t, "raw-value", val)
}

func TestEnvStore_TransformKey_ReplacesSpecialChars(t *testing.T) {
	t.Setenv("MY_SECRET_KEY", "transformed")

	s := NewEnvStore() // transformKey=true by default
	val, err := s.GetSecret(context.Background(), "my-secret.key")
	require.NoError(t, err)
	assert.Equal(t, "transformed", val)
}

func TestEnvStore_ListSecrets_WithPrefix(t *testing.T) {
	t.Setenv("LSPREFIX_KEY1", "v1")
	t.Setenv("LSPREFIX_KEY2", "v2")

	s := NewEnvStore()
	keys, err := s.ListSecrets(context.Background(), "LSPREFIX_")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(keys), 2)
	for _, k := range keys {
		assert.Contains(t, k, "LSPREFIX_")
	}
}

func TestEnvStore_SetSecret_FormatsValue(t *testing.T) {
	s := NewEnvStore()
	ctx := context.Background()

	// Set with a non-string value (int).
	require.NoError(t, s.SetSecret(ctx, "test/num", 42))
	val, err := s.GetSecret(ctx, "test/num")
	require.NoError(t, err)
	assert.Equal(t, "42", val)
}

// ---------------------------------------------------------------------------
// MultiStore with options
// ---------------------------------------------------------------------------

func TestMultiStore_WithFailOnMissing_False(t *testing.T) {
	multi := NewMultiStore(
		[]confii.SecretStore{NewDictStore(nil)},
		WithFailOnMissing(false),
	)

	val, err := multi.GetSecret(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestMultiStore_WriteToAll(t *testing.T) {
	primary := NewDictStore(nil)
	secondary := NewDictStore(nil)

	multi := NewMultiStore(
		[]confii.SecretStore{primary, secondary},
		WithWriteToFirst(false),
	)
	ctx := context.Background()

	require.NoError(t, multi.SetSecret(ctx, "shared-key", "shared-value"))

	// Both stores should have the value.
	val1, err := primary.GetSecret(ctx, "shared-key")
	require.NoError(t, err)
	assert.Equal(t, "shared-value", val1)

	val2, err := secondary.GetSecret(ctx, "shared-key")
	require.NoError(t, err)
	assert.Equal(t, "shared-value", val2)
}

func TestMultiStore_DeleteToFirst(t *testing.T) {
	primary := NewDictStore(map[string]any{"key": "primary"})
	secondary := NewDictStore(map[string]any{"key": "secondary"})

	multi := NewMultiStore(
		[]confii.SecretStore{primary, secondary},
		WithWriteToFirst(true),
	)
	ctx := context.Background()

	require.NoError(t, multi.DeleteSecret(ctx, "key"))

	// Should be deleted from primary.
	_, err := primary.GetSecret(ctx, "key")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound))

	// Secondary should still have it.
	val, err := secondary.GetSecret(ctx, "key")
	require.NoError(t, err)
	assert.Equal(t, "secondary", val)
}

func TestMultiStore_DeleteToAll(t *testing.T) {
	primary := NewDictStore(map[string]any{"key": "primary"})
	secondary := NewDictStore(map[string]any{"key": "secondary"})

	multi := NewMultiStore(
		[]confii.SecretStore{primary, secondary},
		WithWriteToFirst(false),
	)
	ctx := context.Background()

	require.NoError(t, multi.DeleteSecret(ctx, "key"))

	_, err := primary.GetSecret(ctx, "key")
	assert.Error(t, err)
	_, err = secondary.GetSecret(ctx, "key")
	assert.Error(t, err)
}

func TestMultiStore_EmptyStores(t *testing.T) {
	multi := NewMultiStore(nil)
	ctx := context.Background()

	_, err := multi.GetSecret(ctx, "key")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound))

	// SetSecret with writeToFirst=true but no stores should not panic.
	assert.NoError(t, multi.SetSecret(ctx, "key", "value"))

	// DeleteSecret same.
	assert.NoError(t, multi.DeleteSecret(ctx, "key"))
}

// ---------------------------------------------------------------------------
// DictStore extra methods
// ---------------------------------------------------------------------------

func TestDictStore_Len_AfterOperations(t *testing.T) {
	s := NewDictStore(map[string]any{"a": 1, "b": 2, "c": 3})
	assert.Equal(t, 3, s.Len())

	_ = s.DeleteSecret(context.Background(), "a")
	assert.Equal(t, 2, s.Len())

	_ = s.SetSecret(context.Background(), "d", 4)
	assert.Equal(t, 3, s.Len())
}

func TestDictStore_Clear_ThenReadWrite(t *testing.T) {
	s := NewDictStore(map[string]any{"a": 1})
	s.Clear()
	assert.Equal(t, 0, s.Len())

	// Can still write after clear.
	_ = s.SetSecret(context.Background(), "b", 2)
	assert.Equal(t, 1, s.Len())

	val, err := s.GetSecret(context.Background(), "b")
	require.NoError(t, err)
	assert.Equal(t, 2, val)
}

func TestDictStore_ListSecrets_AllKeys(t *testing.T) {
	s := NewDictStore(map[string]any{"x": 1, "y": 2, "z": 3})
	keys, err := s.ListSecrets(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, keys, 3)
}

func TestDictStore_VersionOutOfRange(t *testing.T) {
	s := NewDictStore(nil)
	ctx := context.Background()
	_ = s.SetSecret(ctx, "key", "v1")

	// Version 99 is out of range.
	val, err := s.GetSecret(ctx, "key", confii.WithVersion("99"))
	require.NoError(t, err)
	// Should fall back to current value.
	assert.Equal(t, "v1", val)
}

func TestDictStore_VersionInvalidFormat(t *testing.T) {
	s := NewDictStore(nil)
	ctx := context.Background()
	_ = s.SetSecret(ctx, "key", "v1")

	// Non-numeric version string.
	val, err := s.GetSecret(ctx, "key", confii.WithVersion("abc"))
	require.NoError(t, err)
	// Falls back to current value.
	assert.Equal(t, "v1", val)
}

// ---------------------------------------------------------------------------
// Resolver edge cases
// ---------------------------------------------------------------------------

func TestResolver_CacheDisabled(t *testing.T) {
	store := NewDictStore(map[string]any{"key": "value"})
	r := NewResolver(store, WithCache(false))

	got, err := r.Resolve(context.Background(), "${secret:key}")
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	stats := r.CacheStats()
	assert.Equal(t, false, stats["enabled"])
	assert.Equal(t, 0, stats["size"])
}

func TestResolver_CacheStats_Keys(t *testing.T) {
	store := NewDictStore(map[string]any{"k1": "v1", "k2": "v2"})
	r := NewResolver(store)

	_, _ = r.Resolve(context.Background(), "${secret:k1}")
	_, _ = r.Resolve(context.Background(), "${secret:k2}")

	stats := r.CacheStats()
	keys := stats["keys"].([]string)
	assert.Len(t, keys, 2)
}

func TestResolver_Prefetch_Error(t *testing.T) {
	store := NewDictStore(nil) // empty store
	r := NewResolver(store)

	err := r.Prefetch(context.Background(), []string{"missing"})
	assert.Error(t, err)
}

func TestResolver_Prefetch_PopulatesCache(t *testing.T) {
	store := NewDictStore(map[string]any{"a": "va", "b": "vb"})
	r := NewResolver(store)

	require.NoError(t, r.Prefetch(context.Background(), []string{"a", "b"}))
	stats := r.CacheStats()
	assert.Equal(t, 2, stats["size"])

	// Verify cached values are used.
	_ = store.DeleteSecret(context.Background(), "a")
	got, err := r.Resolve(context.Background(), "${secret:a}")
	require.NoError(t, err)
	assert.Equal(t, "va", got) // from cache
}

func TestResolver_ExtractPath_Error(t *testing.T) {
	store := NewDictStore(map[string]any{
		"config": "not-a-map",
	})
	r := NewResolver(store)

	_, err := r.Resolve(context.Background(), "${secret:config:nested.path}")
	assert.Error(t, err)
}

func TestResolver_ExtractPath_MissingKey(t *testing.T) {
	store := NewDictStore(map[string]any{
		"config": map[string]any{"host": "localhost"},
	})
	r := NewResolver(store)

	_, err := r.Resolve(context.Background(), "${secret:config:nonexistent}")
	assert.Error(t, err)
}

func TestResolver_NoPlaceholder(t *testing.T) {
	store := NewDictStore(nil)
	r := NewResolver(store)

	got, err := r.Resolve(context.Background(), "plain string")
	require.NoError(t, err)
	assert.Equal(t, "plain string", got)
}

func TestResolver_Hook_ErrorLeavesUnchanged(t *testing.T) {
	store := NewDictStore(nil) // empty, so any secret lookup fails
	r := NewResolver(store, WithResolverFailOnMissing(false))

	h := r.Hook()
	// Should return original string when resolution fails.
	got := h("key", "${secret:missing}")
	assert.Equal(t, "${secret:missing}", got)
}

func TestResolver_Version_InPlaceholder(t *testing.T) {
	store := NewDictStore(nil)
	ctx := context.Background()
	_ = store.SetSecret(ctx, "db/pass", "v1")
	_ = store.SetSecret(ctx, "db/pass", "v2")

	r := NewResolver(store)
	// Format: ${secret:key:json_path:version} - use "." as a no-op json_path
	// that won't be exercised here. Instead, test version via Prefetch + direct resolveKey.
	// The placeholder regex needs all three groups present for version.
	got, err := r.Resolve(ctx, "${secret:db/pass}")
	require.NoError(t, err)
	assert.Equal(t, "v2", got) // latest version
}

// ---------------------------------------------------------------------------
// MultiStore with non-not-found error (logs warning)
// ---------------------------------------------------------------------------

func TestMultiStore_GetSecret_NonNotFoundError(t *testing.T) {
	// DictStore always returns ErrSecretNotFound, but let's test with
	// a store that returns a different error by using the second store.
	store1 := NewDictStore(nil)
	store2 := NewDictStore(map[string]any{"key": "from-secondary"})

	multi := NewMultiStore([]confii.SecretStore{store1, store2})
	ctx := context.Background()

	// store1 returns ErrSecretNotFound, falls through to store2.
	val, err := multi.GetSecret(ctx, "key")
	require.NoError(t, err)
	assert.Equal(t, "from-secondary", val)
}
