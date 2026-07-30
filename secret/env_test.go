// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
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

		matching := make(map[string]bool, len(keys))
		for _, k := range keys {
			matching[k] = true
		}
		assert.True(t, matching[prefix+"ALPHA"], "ALPHA must be listed")
		assert.True(t, matching[prefix+"BETA"], "BETA must be listed")
		assert.True(t, matching[prefix+"GAMMA"], "GAMMA must be listed")

		for _, k := range keys {
			assert.True(t, strings.HasPrefix(k, prefix),
				"prefix filter leaked non-matching key %q", k)
		}
	})

	t.Run("non-matching prefix returns no keys", func(t *testing.T) {

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
