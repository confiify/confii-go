// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/secretref"
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

	_ = store.SetSecret(ctx, "key", "updated")

	got, _ = r.Resolve(ctx, "${secret:key}")
	assert.Equal(t, "original", got)

	time.Sleep(60 * time.Millisecond)
	got, _ = r.Resolve(ctx, "${secret:key}")
	assert.Equal(t, "updated", got)
}

func TestResolver_Hook(t *testing.T) {
	store := NewDictStore(map[string]any{"api/key": "resolved"})
	r := NewResolver(store)

	h := r.Hook()
	got, err := h(context.Background(), "key", "${secret:api/key}")
	require.NoError(t, err)
	assert.Equal(t, "resolved", got)

	got, err = h(context.Background(), "key", 42)
	require.NoError(t, err)
	assert.Equal(t, 42, got)
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

func TestResolver_MissingAlwaysFails(t *testing.T) {
	store := NewDictStore(nil)
	r := NewResolver(store)

	got, err := r.Resolve(context.Background(), "${secret:missing}")
	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.Equal(t, "${secret:missing}", got)
}

func TestResolver_Hook_ResolveError(t *testing.T) {
	store := NewDictStore(nil)
	r := NewResolver(store)

	h := r.Hook()

	got, err := h(context.Background(), "key", "${secret:missing_key}")
	require.Error(t, err)
	assert.Equal(t, "${secret:missing_key}", got)
}

func TestResolver_VersionedSecretFetch(t *testing.T) {
	store := NewDictStore(nil)
	ctx := context.Background()

	_ = store.SetSecret(ctx, "db/pass", "version0")
	_ = store.SetSecret(ctx, "db/pass", "version1")
	_ = store.SetSecret(ctx, "db/pass", "version2_latest")

	r := NewResolver(store, WithCache(false))

	got, err := r.Resolve(ctx, "${secret:db/pass}")
	require.NoError(t, err)
	assert.Equal(t, "version2_latest", got)

	got, err = r.Resolve(ctx, "${secret:db/pass::0}")
	require.NoError(t, err)
	assert.Equal(t, "version0", got)

	got, err = r.Resolve(ctx, "${secret:db/pass::1}")
	require.NoError(t, err)
	assert.Equal(t, "version1", got)

}

type recordingStore struct {
	value     any
	lastKey   string
	lastOpts  confii.SecretOptions
	callCount int
	returnErr error
}

func (s *recordingStore) GetSecret(_ context.Context, key string, opts ...confii.SecretOption) (any, error) {
	s.lastKey = key
	s.lastOpts = confii.ResolveSecretOptions(opts...)
	s.callCount++
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	return s.value, nil
}

func (s *recordingStore) SetSecret(_ context.Context, _ string, _ any, _ ...confii.SecretOption) error {
	return nil
}

func (s *recordingStore) DeleteSecret(_ context.Context, _ string, _ ...confii.SecretOption) error {
	return nil
}

func (s *recordingStore) ListSecrets(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func TestResolver_PropagatesVersionToStore(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantKey     string
		wantVersion string
	}{
		{"no version", "${secret:db/pass}", "db/pass", ""},
		{"version with path", "${secret:db/pass:host:7}", "db/pass", "7"},
		{"version with empty path", "${secret:db/pass::3}", "db/pass", "3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := &recordingStore{value: "ok"}
			r := NewResolver(rs, WithCache(false))
			_, _ = r.Resolve(context.Background(), tc.input)
			assert.Equal(t, tc.wantKey, rs.lastKey)
			assert.Equal(t, tc.wantVersion, rs.lastOpts.Version,
				"resolver did not propagate captured version to store options")
		})
	}
}

type contextRecordingStore struct {
	value   any
	lastCtx context.Context
}

func (s *contextRecordingStore) GetSecret(ctx context.Context, _ string, _ ...confii.SecretOption) (any, error) {
	s.lastCtx = ctx
	return s.value, nil
}

func (s *contextRecordingStore) SetSecret(_ context.Context, _ string, _ any, _ ...confii.SecretOption) error {
	return nil
}

func (s *contextRecordingStore) DeleteSecret(_ context.Context, _ string, _ ...confii.SecretOption) error {
	return nil
}

func (s *contextRecordingStore) ListSecrets(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

type ctxKey struct{}

func TestResolverHook_PropagatesCallerContext(t *testing.T) {
	store := &contextRecordingStore{value: "ok"}
	r := NewResolver(store, WithCache(false))

	h := r.Hook()
	wantCtx := context.WithValue(context.Background(), ctxKey{}, "caller-marker")

	got, err := h(wantCtx, "key", "${secret:db/pass}")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)

	require.NotNil(t, store.lastCtx, "store did not receive any context")
	assert.Equal(t, "caller-marker", store.lastCtx.Value(ctxKey{}),
		"resolver did not propagate caller context to the store")
}

func TestResolverHook_PropagatesCallerContext_Cancellation(t *testing.T) {
	store := &contextRecordingStore{value: "ok"}
	r := NewResolver(store, WithCache(false))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Hook()(ctx, "key", "${secret:db/pass}")

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, store.lastCtx, "a canceled operation must not contact the store")
}

func TestResolverHook_FailOnMissing_ReturnsError(t *testing.T) {
	store := NewDictStore(nil)
	r := NewResolver(store)

	got, err := r.Hook()(context.Background(), "api.key", "${secret:missing_key}")

	require.Error(t, err, "Hook must surface resolution error when failOnMissing=true")
	assert.ErrorIs(t, err, confii.ErrSecretNotFound)

	assert.Equal(t, "${secret:missing_key}", got)
}

func TestResolverHook_MissingAlwaysReturnsError(t *testing.T) {
	store := NewDictStore(nil)
	r := NewResolver(store)

	got, err := r.Hook()(context.Background(), "api.key", "${secret:missing_key}")

	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.Equal(t, "${secret:missing_key}", got)
}

func TestResolverHook_ContextContract(t *testing.T) {
	store := NewDictStore(map[string]any{"api/key": "resolved"})
	r := NewResolver(store)

	h := r.Hook()
	got, err := h(context.Background(), "key", "${secret:api/key}")
	require.NoError(t, err)
	assert.Equal(t, "resolved", got)

	missingResolver := NewResolver(NewDictStore(nil))
	got, err = missingResolver.Hook()(context.Background(), "key", "${secret:missing}")
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.Equal(t, "${secret:missing}", got)
}

func TestResolverHook_FailOnMissing_PartialResolution(t *testing.T) {
	store := NewDictStore(map[string]any{"present": "found"})
	r := NewResolver(store)

	_, err := r.Hook()(context.Background(), "k",
		"a=${secret:present} b=${secret:absent}")

	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"resolution error must wrap ErrSecretNotFound, got %v", err)
}

type orderedFailStore struct {
	missKey   string
	value     any
	callKeys  []string
	callCount int
}

func (s *orderedFailStore) GetSecret(_ context.Context, key string, _ ...confii.SecretOption) (any, error) {
	s.callKeys = append(s.callKeys, key)
	s.callCount++
	if key == s.missKey {
		return nil, confii.ErrSecretNotFound
	}
	return s.value, nil
}

func (s *orderedFailStore) SetSecret(_ context.Context, _ string, _ any, _ ...confii.SecretOption) error {
	return nil
}

func (s *orderedFailStore) DeleteSecret(_ context.Context, _ string, _ ...confii.SecretOption) error {
	return nil
}

func (s *orderedFailStore) ListSecrets(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func TestResolver_FailOnMissing_ShortCircuitsOnFirstError(t *testing.T) {
	store := &orderedFailStore{missKey: "absent", value: "ok"}
	r := NewResolver(store, WithCache(false))

	input := "first=${secret:absent} second=${secret:present}"
	got, err := r.Resolve(context.Background(), input)

	require.Error(t, err, "failOnMissing=true must surface the first failure")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"error must wrap ErrSecretNotFound, got %v", err)
	assert.Equal(t, input, got,
		"input must be returned verbatim under fail-fast: no partial substitution")
	assert.Equal(t, 1, store.callCount,
		"store must be queried only for the first failing placeholder, got %d calls (keys=%v)",
		store.callCount, store.callKeys)
	require.Len(t, store.callKeys, 1)
	assert.Equal(t, "absent", store.callKeys[0],
		"only the first (failing) placeholder may reach the store")
}

func TestResolver_MissingShortCircuits(t *testing.T) {
	store := &orderedFailStore{missKey: "absent", value: "ok"}
	r := NewResolver(store, WithCache(false))

	input := "first=${secret:absent} second=${secret:present}"
	got, err := r.Resolve(context.Background(), input)

	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.Equal(t, input, got)
	assert.Equal(t, 1, store.callCount)
	assert.Equal(t, []string{"absent"}, store.callKeys)
}

func TestResolver_FailOnMissing_HookPath_AlsoShortCircuits(t *testing.T) {
	store := &orderedFailStore{missKey: "absent", value: "ok"}
	r := NewResolver(store, WithCache(false))

	input := "first=${secret:absent} second=${secret:present}"
	got, err := r.Hook()(context.Background(), "k", input)

	require.Error(t, err, "Hook must surface the first failure under failOnMissing=true")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"error must wrap ErrSecretNotFound, got %v", err)

	assert.Equal(t, input, got,
		"Hook error path must return the original input, never a partial substitution")
	assert.Equal(t, 1, store.callCount,
		"Hook path must also short-circuit at the first failing placeholder")
}

func TestResolver_PlaceholderForms(t *testing.T) {
	tests := []struct {
		input   string
		matches bool
		key     string
		path    string
		version string
	}{
		{"${secret:key}", true, "key", "", ""},
		{"${secret:db/password}", true, "db/password", "", ""},
		{"${secret:key:path}", true, "key", "path", ""},
		{"${secret:key:path.to.field}", true, "key", "path.to.field", ""},
		{"${secret:key:path:v1}", true, "key", "path", "v1"},
		{"${secret:key::v1}", true, "key", "", "v1"},

		{"${secret:key:}", true, "key", "", ""},
		{"${secret:key::}", false, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			groups := secretPattern.FindStringSubmatch(tt.input)
			if !tt.matches {
				assert.Nil(t, groups, "expected no match for %q", tt.input)
				return
			}
			require.NotNil(t, groups, "expected match for %q", tt.input)
			// Read through the shared accessor rather than by index: the
			// grammar gained a provider group, and index arithmetic in tests
			// is how that kind of change goes unnoticed.
			ref := secretref.FromMatch(groups)
			assert.Equal(t, tt.key, ref.Key)
			assert.Equal(t, tt.path, ref.Field)
			assert.Equal(t, tt.version, ref.Version)
		})
	}
}
