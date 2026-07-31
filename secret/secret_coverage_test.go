// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	s := NewEnvStore()
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

	require.NoError(t, s.SetSecret(ctx, "test/num", 42))
	val, err := s.GetSecret(ctx, "test/num")
	require.NoError(t, err)
	assert.Equal(t, "42", val)
}

func TestMultiStore_MissingIsTypedError(t *testing.T) {
	multi := NewMultiStore([]confii.SecretStore{NewDictStore(nil)})

	val, err := multi.GetSecret(context.Background(), "missing")
	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.Nil(t, val)
}

func TestMultiStore_WriteToAll(t *testing.T) {
	primary := NewDictStore(nil)
	secondary := NewDictStore(nil)

	multi := NewMultiStore([]confii.SecretStore{primary, secondary},
		WithWriteToFirst(false),
	)
	ctx := context.Background()

	require.NoError(t, multi.SetSecret(ctx, "shared-key", "shared-value"))

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

	multi := NewMultiStore([]confii.SecretStore{primary, secondary},
		WithWriteToFirst(true),
	)
	ctx := context.Background()

	require.NoError(t, multi.DeleteSecret(ctx, "key"))

	_, err := primary.GetSecret(ctx, "key")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound))

	val, err := secondary.GetSecret(ctx, "key")
	require.NoError(t, err)
	assert.Equal(t, "secondary", val)
}

func TestMultiStore_DeleteToAll(t *testing.T) {
	primary := NewDictStore(map[string]any{"key": "primary"})
	secondary := NewDictStore(map[string]any{"key": "secondary"})

	multi := NewMultiStore([]confii.SecretStore{primary, secondary},
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

	assert.NoError(t, multi.SetSecret(ctx, "key", "value"))

	assert.NoError(t, multi.DeleteSecret(ctx, "key"))
}

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

	val, err := s.GetSecret(ctx, "key", confii.WithVersion("99"))
	assert.Nil(t, val)
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"out-of-range version should yield ErrSecretNotFound, got %v", err)
}

func TestDictStore_VersionInvalidFormat(t *testing.T) {
	s := NewDictStore(nil)
	ctx := context.Background()
	_ = s.SetSecret(ctx, "key", "v1")

	val, err := s.GetSecret(ctx, "key", confii.WithVersion("abc"))
	assert.Nil(t, val)
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"unparseable version should yield ErrSecretNotFound, got %v", err)
}

func TestDictStore_VersionNegative(t *testing.T) {
	s := NewDictStore(nil)
	ctx := context.Background()
	_ = s.SetSecret(ctx, "key", "v1")

	val, err := s.GetSecret(ctx, "key", confii.WithVersion("-1"))
	assert.Nil(t, val)
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"negative version should yield ErrSecretNotFound, got %v", err)
}

func TestDictStore_VersionNoHistory(t *testing.T) {

	s := NewDictStore(map[string]any{"key": "current"})
	ctx := context.Background()

	val, err := s.GetSecret(ctx, "key", confii.WithVersion("0"))
	assert.Nil(t, val)
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"version request against no-history key should yield ErrSecretNotFound, got %v", err)
}

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
	store := NewDictStore(nil)
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

	_ = store.DeleteSecret(context.Background(), "a")
	got, err := r.Resolve(context.Background(), "${secret:a}")
	require.NoError(t, err)
	assert.Equal(t, "va", got)
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
	store := NewDictStore(nil)
	r := NewResolver(store)

	h := r.Hook()

	got, err := h(context.Background(), "key", "${secret:missing}")
	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.Equal(t, "${secret:missing}", got)
}

func TestResolver_Version_InPlaceholder(t *testing.T) {
	store := NewDictStore(nil)
	ctx := context.Background()
	_ = store.SetSecret(ctx, "db/pass", "v0")
	_ = store.SetSecret(ctx, "db/pass", "v1")
	_ = store.SetSecret(ctx, "db/pass", "v2")

	r := NewResolver(store, WithCache(false))

	got, err := r.Resolve(ctx, "${secret:db/pass}")
	require.NoError(t, err)
	assert.Equal(t, "v2", got)

	got, err = r.Resolve(ctx, "${secret:db/pass::0}")
	require.NoError(t, err)
	assert.Equal(t, "v0", got)

	got, err = r.Resolve(ctx, "${secret:db/pass::1}")
	require.NoError(t, err)
	assert.Equal(t, "v1", got)
}

func TestMultiStore_GetSecret_NonNotFoundError(t *testing.T) {

	store1 := NewDictStore(nil)
	store2 := NewDictStore(map[string]any{"key": "from-secondary"})

	multi := NewMultiStore([]confii.SecretStore{store1, store2})
	ctx := context.Background()

	val, err := multi.GetSecret(ctx, "key")
	require.NoError(t, err)
	assert.Equal(t, "from-secondary", val)
}

type failingStore struct {
	err error
}

func (s *failingStore) GetSecret(_ context.Context, _ string, _ ...confii.SecretOption) (any, error) {
	return nil, s.err
}

func (s *failingStore) SetSecret(_ context.Context, _ string, _ any, _ ...confii.SecretOption) error {
	return s.err
}

func (s *failingStore) DeleteSecret(_ context.Context, _ string, _ ...confii.SecretOption) error {
	return s.err
}

func (s *failingStore) ListSecrets(_ context.Context, _ string) ([]string, error) {
	return nil, s.err
}

func TestMultiStore_GetSecret_NonNotFoundError_FailingStore(t *testing.T) {
	fail := &failingStore{err: errors.New("connection refused")}
	working := NewDictStore(map[string]any{"key": "found"})

	multi := NewMultiStore([]confii.SecretStore{fail, working})
	ctx := context.Background()

	val, err := multi.GetSecret(ctx, "key")
	require.Error(t, err)
	assert.Nil(t, val)
}

func TestMultiStore_SetSecret_ChainFailure(t *testing.T) {
	working := NewDictStore(nil)
	fail := &failingStore{err: errors.New("write error")}

	multi := NewMultiStore([]confii.SecretStore{working, fail},
		WithWriteToFirst(false),
	)

	err := multi.SetSecret(context.Background(), "key", "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write error")
}

func TestMultiStore_DeleteSecret_ChainFailure(t *testing.T) {
	working := NewDictStore(map[string]any{"key": "val"})
	fail := &failingStore{err: errors.New("delete error")}

	multi := NewMultiStore([]confii.SecretStore{working, fail},
		WithWriteToFirst(false),
	)

	err := multi.DeleteSecret(context.Background(), "key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete error")
}

func TestMultiStore_ListSecrets_StoreError(t *testing.T) {
	listErr := errors.New("list error")
	fail := &failingStore{err: listErr}
	working := NewDictStore(map[string]any{"key1": "v1", "key2": "v2"})

	multi := NewMultiStore([]confii.SecretStore{fail, working})

	keys, err := multi.ListSecrets(context.Background(), "")
	require.Error(t, err, "store error must be surfaced, not silently skipped")
	assert.True(t, errors.Is(err, listErr))

	assert.GreaterOrEqual(t, len(keys), 2)
}
