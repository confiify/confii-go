package secret

import (
	"context"
	"errors"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDictStore_CRUD(t *testing.T) {
	s := NewDictStore(map[string]any{"key1": "value1"})
	ctx := context.Background()

	// Get existing.
	val, err := s.GetSecret(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)

	// Set new.
	require.NoError(t, s.SetSecret(ctx, "key2", "value2"))
	val, err = s.GetSecret(ctx, "key2")
	require.NoError(t, err)
	assert.Equal(t, "value2", val)

	// Delete.
	require.NoError(t, s.DeleteSecret(ctx, "key1"))
	_, err = s.GetSecret(ctx, "key1")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound))

	// Len.
	assert.Equal(t, 1, s.Len())
}

func TestDictStore_Versioning(t *testing.T) {
	s := NewDictStore(nil)
	ctx := context.Background()

	s.SetSecret(ctx, "key", "v1")
	s.SetSecret(ctx, "key", "v2")
	s.SetSecret(ctx, "key", "v3")

	// Latest.
	val, _ := s.GetSecret(ctx, "key")
	assert.Equal(t, "v3", val)

	// Version 0.
	val, _ = s.GetSecret(ctx, "key", confii.WithVersion("0"))
	assert.Equal(t, "v1", val)

	// Version 1.
	val, _ = s.GetSecret(ctx, "key", confii.WithVersion("1"))
	assert.Equal(t, "v2", val)
}

func TestDictStore_ListSecrets(t *testing.T) {
	s := NewDictStore(map[string]any{
		"db/host":     "localhost",
		"db/password": "secret",
		"api/key":     "abc",
	})

	keys, err := s.ListSecrets(context.Background(), "db/")
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestDictStore_Clear(t *testing.T) {
	s := NewDictStore(map[string]any{"a": 1})
	s.Clear()
	assert.Equal(t, 0, s.Len())
}
