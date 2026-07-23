package secret

import (
	"context"
	"errors"
	"testing"
	"time"

	confii "github.com/confiify/confii-go"
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
	// Set multiple versions: indexes 0, 1, 2.
	_ = store.SetSecret(ctx, "db/pass", "version0")
	_ = store.SetSecret(ctx, "db/pass", "version1")
	_ = store.SetSecret(ctx, "db/pass", "version2_latest")

	r := NewResolver(store, WithCache(false))

	// No version captured -> current/latest value.
	got, err := r.Resolve(ctx, "${secret:db/pass}")
	require.NoError(t, err)
	assert.Equal(t, "version2_latest", got)

	// Versioned form with empty json_path: ${secret:key::version}
	got, err = r.Resolve(ctx, "${secret:db/pass::0}")
	require.NoError(t, err)
	assert.Equal(t, "version0", got)

	got, err = r.Resolve(ctx, "${secret:db/pass::1}")
	require.NoError(t, err)
	assert.Equal(t, "version1", got)

	// Versioned form with json_path also exercises the same propagation path.
	// Use a value whose path traversal is a no-op by storing a map at index 1
	// alongside the existing string versions; instead we just confirm the
	// regex captures version even when path is non-empty by routing through
	// a recording store (see TestResolver_PropagatesVersionToStore below).
}

// recordingStore captures the SecretOptions handed to GetSecret so tests can
// assert that the resolver actually plumbs the captured placeholder version
// through to the underlying store API.
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

// TestResolver_PropagatesVersionToStore is the version-propagation test the
// audit found missing: it asserts the resolver hands the version captured
// from the placeholder through to the store via WithVersion.
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

// ---------------------------------------------------------------------------
// G23: HookCtx propagates context and surfaces errors when failOnMissing.
// ---------------------------------------------------------------------------

// contextRecordingStore records the context handed to each GetSecret call so
// tests can verify the resolver propagates the caller's context instead of
// substituting context.Background().
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

// TestResolverHook_PropagatesCallerContext exercises the new context-aware
// hook variant and asserts that values plumbed through ctx reach the
// underlying store. The legacy [Resolver.Hook] cannot do this because its
// signature does not accept a context.
func TestResolverHook_PropagatesCallerContext(t *testing.T) {
	store := &contextRecordingStore{value: "ok"}
	r := NewResolver(store, WithCache(false))

	h := r.HookCtx()
	wantCtx := context.WithValue(context.Background(), ctxKey{}, "caller-marker")

	got, err := h(wantCtx, "key", "${secret:db/pass}")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)

	require.NotNil(t, store.lastCtx, "store did not receive any context")
	assert.Equal(t, "caller-marker", store.lastCtx.Value(ctxKey{}),
		"resolver did not propagate caller context to the store")
}

// TestResolverHook_PropagatesCallerContext_Cancellation confirms that ctx
// cancellation reaches the store (proving the hook is not substituting
// context.Background()).
func TestResolverHook_PropagatesCallerContext_Cancellation(t *testing.T) {
	store := &contextRecordingStore{value: "ok"}
	r := NewResolver(store, WithCache(false))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel; store should observe it

	_, _ = r.HookCtx()(ctx, "key", "${secret:db/pass}")

	require.NotNil(t, store.lastCtx)
	assert.ErrorIs(t, store.lastCtx.Err(), context.Canceled,
		"store should have received an already-canceled context")
}

// TestResolverHook_FailOnMissing_ReturnsError confirms that with
// WithResolverFailOnMissing(true) (the default), an unresolvable
// placeholder produces an error wrapping ErrSecretNotFound through the
// context-aware hook path. Before G23 was fixed, the hook silently
// returned the unresolved placeholder.
func TestResolverHook_FailOnMissing_ReturnsError(t *testing.T) {
	store := NewDictStore(nil) // empty: any lookup misses
	r := NewResolver(store, WithResolverFailOnMissing(true))

	got, err := r.HookCtx()(context.Background(), "api.key", "${secret:missing_key}")

	require.Error(t, err, "HookCtx must surface resolution error when failOnMissing=true")
	assert.ErrorIs(t, err, confii.ErrSecretNotFound)
	// The hook returns the original (unresolved) value alongside the error so
	// callers that elect to ignore err can fall back to the placeholder.
	assert.Equal(t, "${secret:missing_key}", got)
}

// TestResolverHook_FailOnMissing_Default_LegacyBehavior asserts that with
// WithResolverFailOnMissing(false) the hook preserves the legacy behavior:
// no error, original placeholder unchanged.
func TestResolverHook_FailOnMissing_Default_LegacyBehavior(t *testing.T) {
	store := NewDictStore(nil)
	r := NewResolver(store, WithResolverFailOnMissing(false))

	got, err := r.HookCtx()(context.Background(), "api.key", "${secret:missing_key}")

	require.NoError(t, err, "HookCtx must not raise when failOnMissing=false")
	assert.Equal(t, "${secret:missing_key}", got)
}

// TestResolverHook_BackwardCompatHookSignature pins down the contract that
// existing third-party hooks implementing the legacy hook.Func signature
// continue to work and that Resolver.Hook itself remains usable.
func TestResolverHook_BackwardCompatHookSignature(t *testing.T) {
	store := NewDictStore(map[string]any{"api/key": "resolved"})
	r := NewResolver(store)

	legacy := r.Hook() // legacy signature: func(string, any) any
	got := legacy("key", "${secret:api/key}")
	assert.Equal(t, "resolved", got, "legacy hook must still resolve placeholders")

	// Even with failOnMissing=true the legacy hook MUST NOT panic and MUST
	// return the original value (its signature cannot carry an error).
	missingResolver := NewResolver(NewDictStore(nil), WithResolverFailOnMissing(true))
	legacy = missingResolver.Hook()
	got = legacy("key", "${secret:missing}")
	assert.Equal(t, "${secret:missing}", got,
		"legacy hook signature cannot surface errors; original value preserved")
}

// TestResolverHook_FailOnMissing_PartialResolution confirms that when one
// placeholder in a string fails and another would succeed, HookCtx returns
// the resolution error rather than a partially substituted value.
func TestResolverHook_FailOnMissing_PartialResolution(t *testing.T) {
	store := NewDictStore(map[string]any{"present": "found"})
	r := NewResolver(store, WithResolverFailOnMissing(true))

	_, err := r.HookCtx()(context.Background(), "k",
		"a=${secret:present} b=${secret:absent}")

	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"resolution error must wrap ErrSecretNotFound, got %v", err)
}

// ---------------------------------------------------------------------------
// D02: failOnMissing=true must short-circuit at the FIRST failing placeholder.
// Before the fix Resolver.Resolve iterated every placeholder, applied
// substitutions for the successful ones, and only surfaced the *last* error.
// That hides early failures and contradicts the fail-fast contract documented
// on WithResolverFailOnMissing(true).
// ---------------------------------------------------------------------------

// orderedFailStore returns ErrSecretNotFound for the configured "missKey" and
// the recorded value for any other key. It records every key handed to
// GetSecret so tests can assert call ordering and short-circuit semantics.
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

// TestResolver_FailOnMissing_ShortCircuitsOnFirstError pins the D02 fix:
// with failOnMissing=true the resolver must stop on the first failure,
// return the original input verbatim (no partial substitution), and not
// query the store for any later placeholder.
func TestResolver_FailOnMissing_ShortCircuitsOnFirstError(t *testing.T) {
	store := &orderedFailStore{missKey: "absent", value: "ok"}
	r := NewResolver(store, WithResolverFailOnMissing(true), WithCache(false))

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

// TestResolver_FailOnMissing_False_ContinuesPastFailure preserves the
// legacy behavior under failOnMissing=false: missing placeholders are left
// in place but successful ones are still substituted, and the store is
// queried for every placeholder in the input.
func TestResolver_FailOnMissing_False_ContinuesPastFailure(t *testing.T) {
	store := &orderedFailStore{missKey: "absent", value: "ok"}
	r := NewResolver(store, WithResolverFailOnMissing(false), WithCache(false))

	input := "first=${secret:absent} second=${secret:present}"
	got, err := r.Resolve(context.Background(), input)

	require.NoError(t, err, "failOnMissing=false must not raise")
	assert.Equal(t, "first=${secret:absent} second=ok", got,
		"missing placeholder is left verbatim; successful one is substituted")
	assert.Equal(t, 2, store.callCount,
		"legacy mode must continue past the failure and call the store for every placeholder")
	assert.Equal(t, []string{"absent", "present"}, store.callKeys,
		"placeholders must be processed in source order under legacy mode")
}

// TestResolver_FailOnMissing_HookCtxPath_AlsoShortCircuits confirms the
// short-circuit contract is preserved when error propagation flows through
// the Wave 2 G23 HookCtx surface (i.e. the path taken by
// Config.GetCtx callers).
func TestResolver_FailOnMissing_HookCtxPath_AlsoShortCircuits(t *testing.T) {
	store := &orderedFailStore{missKey: "absent", value: "ok"}
	r := NewResolver(store, WithResolverFailOnMissing(true), WithCache(false))

	input := "first=${secret:absent} second=${secret:present}"
	got, err := r.HookCtx()(context.Background(), "k", input)

	require.Error(t, err, "HookCtx must surface the first failure under failOnMissing=true")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound),
		"error must wrap ErrSecretNotFound, got %v", err)
	// HookCtx returns the original (non-mutated) value alongside the
	// error so opt-in callers can fall back to the placeholder text.
	assert.Equal(t, input, got,
		"HookCtx error path must return the original input, never a partial substitution")
	assert.Equal(t, 1, store.callCount,
		"HookCtx path must also short-circuit at the first failing placeholder")
}

// TestResolver_PlaceholderForms enumerates every documented placeholder form
// and asserts each is recognized by the regex (or, for the trailing-colon
// shapes, intentionally not recognized).
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
		// Trailing-colon shapes are not version requests (version requires 1+ chars).
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
			assert.Equal(t, tt.key, groups[1])
			assert.Equal(t, tt.path, groups[2])
			assert.Equal(t, tt.version, groups[3])
		})
	}
}
