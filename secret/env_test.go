package secret

import (
	"context"
	"errors"
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

func TestEnvStore_ListSecrets(t *testing.T) {
	s := NewEnvStore()
	keys, err := s.ListSecrets(context.Background(), "")
	require.NoError(t, err)
	assert.NotEmpty(t, keys)
}
