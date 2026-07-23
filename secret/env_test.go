package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvStore_GetSecret(t *testing.T) {
	t.Setenv("API_KEY", "secret123")

	s := NewEnvStore()
	val, err := s.GetSecret(context.Background(), "api/key")
	require.NoError(t, err)
	assert.Equal(t, "secret123", val)
}

func TestEnvStore_GetSecret_NotFound(t *testing.T) {
	s := NewEnvStore()
	_, err := s.GetSecret(context.Background(), "nonexistent/key")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound))
}

func TestEnvStore_WithPrefix(t *testing.T) {
	t.Setenv("PROD_DB_PASSWORD", "pass123")

	s := NewEnvStore(WithEnvPrefix("PROD_"))
	val, err := s.GetSecret(context.Background(), "db/password")
	require.NoError(t, err)
	assert.Equal(t, "pass123", val)
}

func TestEnvStore_SetAndDelete(t *testing.T) {
	s := NewEnvStore()
	ctx := context.Background()

	require.NoError(t, s.SetSecret(ctx, "test/key", "value"))

	val, err := s.GetSecret(ctx, "test/key")
	require.NoError(t, err)
	assert.Equal(t, "value", val)

	require.NoError(t, s.DeleteSecret(ctx, "test/key"))

	_, err = s.GetSecret(ctx, "test/key")
	assert.Error(t, err)
}

// TestEnvStore_ListSecrets previously read EnvStore.ListSecrets("") and
// only asserted the returned slice was non-empty. That assertion passes
// as long as the test process has ANY environment variable defined
// (PATH, HOME, USER, ...), which means the test would have passed even
// if ListSecrets had been gutted to `return os.Environ(), nil` with no
// filtering, transformation, or even iteration of the configured
// EnvStore. The rewritten test:
//  1. Establishes a deterministic environment via t.Setenv with a
//     unique prefix that cannot collide with ambient process env vars.
//  2. Asserts that filtering by that prefix returns exactly the keys
//     we set (positive control).
//  3. Asserts that filtering by a prefix we did NOT set returns no
//     matching keys (negative control — proves filtering is real).
//  4. Asserts that an unprefixed listing includes our seeded keys
//     (proves the function actually walks os.Environ).
func TestEnvStore_ListSecrets(t *testing.T) {
	const prefix = "CONFII_LISTSECRETS_TEST_"
	t.Setenv(prefix+"ALPHA", "1")
	t.Setenv(prefix+"BETA", "2")
	t.Setenv(prefix+"GAMMA", "3")

	s := NewEnvStore()
	ctx := context.Background()

	t.Run("prefix filter returns only matching keys", func(t *testing.T) {
		keys, err := s.ListSecrets(ctx, prefix)
		require.NoError(t, err)
		// Filter the result down to the prefix to insulate from any
		// other test-injected env vars that happen to share part of
		// the prefix string.
		matching := make(map[string]bool, len(keys))
		for _, k := range keys {
			matching[k] = true
		}
		assert.True(t, matching[prefix+"ALPHA"], "ALPHA must be listed")
		assert.True(t, matching[prefix+"BETA"], "BETA must be listed")
		assert.True(t, matching[prefix+"GAMMA"], "GAMMA must be listed")

		// Strong filter check: every returned key must START with the
		// requested prefix. This catches an implementation that
		// returns os.Environ() unfiltered.
		for _, k := range keys {
			assert.True(t, strings.HasPrefix(k, prefix),
				"prefix filter leaked non-matching key %q", k)
		}
	})

	t.Run("non-matching prefix returns no keys", func(t *testing.T) {
		// Use a prefix we know cannot exist in any test environment.
		const noMatch = "CONFII_LISTSECRETS_NONEXISTENT_PREFIX_"
		keys, err := s.ListSecrets(ctx, noMatch)
		require.NoError(t, err)
		assert.Empty(t, keys,
			"non-matching prefix must yield zero keys; ambient env vars must not leak")
	})

	t.Run("unprefixed listing includes the seeded keys", func(t *testing.T) {
		keys, err := s.ListSecrets(ctx, "")
		require.NoError(t, err)
		require.NotEmpty(t, keys)
		seen := make(map[string]bool, len(keys))
		for _, k := range keys {
			seen[k] = true
		}
		assert.True(t, seen[prefix+"ALPHA"],
			"unprefixed listing must include the keys we set via t.Setenv")
	})
}
