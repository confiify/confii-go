package confii_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Wave 21 G30: access helper semantics surprising/inconsistent.
//
// Audit text:
//   "Keys(prefix) strips the prefix, conflicting with documented
//    cfg.Get(key) loops; Has ignores sysenv fallback that Get can use;
//    GetInt truncates float64; docs claim "1"/"0" bool casting but
//    implementation does not."
//
// Each sub-fix is pinned by one or more tests below. Where the change is a
// behavior tightening (G30.1, G30.3) the legacy assertion in config_test.go
// / config_coverage_test.go has been updated in lock-step with a Wave 21
// rationale comment.
// =============================================================================

// -----------------------------------------------------------------------------
// G30.1 — Keys(prefix) returns FULL prefixed keys.
// -----------------------------------------------------------------------------

// TestG30_Keys_ReturnsFullPrefixedKeys observable: after seeding the config
// with two "db.*" keys and one "redis.*" key, cfg.Keys("db.") returns
// exactly ["db.host","db.port"] (FULL paths) and each returned key is
// directly resolvable via cfg.Get without re-prepending the prefix. Pre-fix
// Keys returned ["host","port"] (stripped), forcing callers to manually
// reconstruct the full path before any Get/Has call.
func TestG30_Keys_ReturnsFullPrefixedKeys(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("db.host", "localhost"))
	require.NoError(t, cfg.Set("db.port", 5432))
	require.NoError(t, cfg.Set("redis.host", "rh"))

	keys := cfg.Keys("db.")
	sort.Strings(keys)
	assert.Equal(t, []string{"db.host", "db.port"}, keys,
		"Keys(prefix) must return FULL prefixed keys (Wave 21 G30.1)")

	// Trailing-dot tolerance: "db" and "db." are equivalent.
	keysNoDot := cfg.Keys("db")
	sort.Strings(keysNoDot)
	assert.Equal(t, keys, keysNoDot,
		`Keys("db") and Keys("db.") must return the same set`)

	// Documented loop-iteration pattern: each returned key feeds directly
	// back into cfg.Get without prefix re-construction.
	for _, k := range keys {
		v, err := cfg.Get(k)
		require.NoError(t, err, "Get(%q) must succeed for a key returned by Keys", k)
		assert.NotNil(t, v)
	}

	// Negative control: Keys("db.") must NOT contain redis.* keys.
	for _, k := range keys {
		assert.False(t, k == "redis.host",
			"Keys(prefix) must filter out non-matching keys")
	}
}

// -----------------------------------------------------------------------------
// G30.2 — Has honors sysenv fallback when Get would.
// -----------------------------------------------------------------------------

// TestG30_Has_HonorsSysenvFallback observable: with sysenv fallback enabled
// and no in-config "db.password" key, setting APP_DB_PASSWORD in the
// process environment makes Has("db.password") return true (matching what
// Get("db.password") returns). Pre-fix Has consulted only the in-memory
// config map and returned false, so Has and Get disagreed.
func TestG30_Has_HonorsSysenvFallback(t *testing.T) {
	t.Setenv("APP_DB_PASSWORD", "secret")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvPrefix("APP"),
		confii.WithSysenvFallback(true),
	)
	require.NoError(t, err)

	// Sanity: Get must resolve via sysenv fallback.
	v, err := cfg.Get("db.password")
	require.NoError(t, err)
	assert.Equal(t, "secret", v)

	// Has must agree.
	assert.True(t, cfg.Has("db.password"),
		"Has must return true when sysenv fallback would resolve the key (Wave 21 G30.2)")

	// Negative control: a key with no in-config and no sysenv backing
	// must still report false.
	assert.False(t, cfg.Has("db.nonexistent.key"),
		"Has must return false when neither config nor sysenv has the key")
}

// TestG30_Has_NoSysenvFallback_StillRespectsOption observable: when sysenv
// fallback is OFF, Has must NOT consult the process environment. This pins
// the symmetric contract: Has follows Get's resolution rules — including
// the WithSysenvFallback toggle — and never overshoots them.
func TestG30_Has_NoSysenvFallback_StillRespectsOption(t *testing.T) {
	t.Setenv("APP_DB_PASSWORD", "secret")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvPrefix("APP"),
		// SysenvFallback intentionally NOT enabled.
	)
	require.NoError(t, err)

	assert.False(t, cfg.Has("db.password"),
		"Has must NOT consult sysenv when SysenvFallback is disabled")
}

// -----------------------------------------------------------------------------
// G30.3 — GetInt rejects floats with non-zero fractional parts.
// -----------------------------------------------------------------------------

// TestG30_GetInt_RejectsNonIntegerFloat observable: GetInt on a stored
// float64 like 3.14 returns a typed *ConfigError wrapping ErrConfigInvalid
// instead of silently truncating to 3. Pre-fix `cfg.Set("port", 3.14);
// cfg.GetInt("port")` returned (3, nil), masking configuration bugs.
func TestG30_GetInt_RejectsNonIntegerFloat(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("port", 3.14))

	v, err := cfg.GetInt("port")
	require.Error(t, err, "GetInt must reject non-integer float (Wave 21 G30.3)")
	assert.Equal(t, 0, v, "rejected GetInt must return zero value")

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"GetInt rejection must wrap ErrConfigInvalid")
	assert.Equal(t, "GetInt", ce.Op)
	assert.Equal(t, "port", ce.Key)
}

// TestG30_GetInt_AcceptsIntegerFloat observable: GetInt on a stored float64
// with a zero fractional part (e.g. 3.0, the natural decode of `3` from
// some JSON / YAML libraries) succeeds and returns the integer value.
func TestG30_GetInt_AcceptsIntegerFloat(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("port", 3.0))
	v, err := cfg.GetInt("port")
	require.NoError(t, err, "GetInt must accept float with zero fractional part")
	assert.Equal(t, 3, v)

	// Negative-valued integer-float must also work (range guard test).
	require.NoError(t, cfg.Set("offset", -7.0))
	v, err = cfg.GetInt("offset")
	require.NoError(t, err)
	assert.Equal(t, -7, v)
}

// TestG30_GetInt_RejectsNonFiniteFloat observable: NaN / +Inf / -Inf inputs
// must produce a typed error rather than UB-style int conversion.
func TestG30_GetInt_RejectsNonFiniteFloat(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("nan", 0.0/zeroFloat()))
	_, err = cfg.GetInt("nan")
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"NaN must be rejected with ErrConfigInvalid")
}

// zeroFloat returns 0 as a float64 in a way the compiler does not constant-
// fold so we can construct NaN / Inf without triggering a "division by
// zero" compile-time error.
func zeroFloat() float64 {
	z := 0.0
	return z
}

// -----------------------------------------------------------------------------
// G30.4 — GetBool accepts all documented string and numeric forms.
// -----------------------------------------------------------------------------

// TestG30_GetBool_AcceptsAllDocumentedForms observable: each documented
// string spelling for boolean (per docs/access.md and the type-casting
// hook docs) coerces correctly via GetBool. Pre-fix only "true"/"false"
// (and a native bool) were accepted; "1"/"0", "yes"/"no", "on"/"off" all
// failed with "cannot convert string to bool".
func TestG30_GetBool_AcceptsAllDocumentedForms(t *testing.T) {
	cases := []struct {
		raw  any
		want bool
	}{
		// Native bool — pre-existing behavior, kept for parity.
		{true, true},
		{false, false},

		// Canonical string spellings.
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{" true ", true}, // trim
		{"false", false},
		{"False", false},

		// Numeric-string forms (docs/access.md WithTypeCasting note).
		{"1", true},
		{"0", false},

		// Yes/No (extended boolean spellings).
		{"yes", true},
		{"YES", true},
		{"no", false},

		// On/Off.
		{"on", true},
		{"off", false},

		// Numeric forms — JSON/YAML often produce these for boolean-like
		// fields; treating them like strings keeps the surface uniform.
		{1, true},
		{0, false},
		{int64(1), true},
		{int64(0), false},
		{1.0, true},
		{0.0, false},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			cfg, err := confii.New[any](context.Background())
			require.NoError(t, err)
			require.NoError(t, cfg.Set("flag", tc.raw))
			got, err := cfg.GetBool("flag")
			require.NoError(t, err, "GetBool must accept %v (%T)", tc.raw, tc.raw)
			assert.Equal(t, tc.want, got, "GetBool(%v) = %v, want %v", tc.raw, got, tc.want)
		})
	}
}

// TestG30_GetBool_RejectsUnknownString observable: a string that doesn't
// match any documented form still returns a typed *ConfigError. This pins
// the negative side of the contract.
func TestG30_GetBool_RejectsUnknownString(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)
	require.NoError(t, cfg.Set("flag", "maybe"))

	_, err = cfg.GetBool("flag")
	require.Error(t, err)
	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.Equal(t, "GetBool", ce.Op)
	assert.Equal(t, "flag", ce.Key)
}
