package secret

import (
	"context"
	"errors"
	"testing"

	confii "github.com/qualitycoe/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiStore_Fallback(t *testing.T) {
	primary := NewDictStore(map[string]any{"key1": "from-primary"})
	secondary := NewDictStore(map[string]any{"key2": "from-secondary"})

	multi := NewMultiStore([]confii.SecretStore{primary, secondary})
	ctx := context.Background()

	val, err := multi.GetSecret(ctx, "key1")
	require.NoError(t, err)
	assert.Equal(t, "from-primary", val)

	val, err = multi.GetSecret(ctx, "key2")
	require.NoError(t, err)
	assert.Equal(t, "from-secondary", val)
}

func TestMultiStore_NotFound(t *testing.T) {
	multi := NewMultiStore([]confii.SecretStore{
		NewDictStore(nil),
	})

	_, err := multi.GetSecret(context.Background(), "missing")
	assert.True(t, errors.Is(err, confii.ErrSecretNotFound))
}

func TestMultiStore_WriteToFirst(t *testing.T) {
	primary := NewDictStore(nil)
	secondary := NewDictStore(nil)

	multi := NewMultiStore([]confii.SecretStore{primary, secondary}, WithWriteToFirst(true))
	ctx := context.Background()

	require.NoError(t, multi.SetSecret(ctx, "key", "value"))

	// Should be in primary only.
	val, _ := primary.GetSecret(ctx, "key")
	assert.Equal(t, "value", val)

	_, err := secondary.GetSecret(ctx, "key")
	assert.Error(t, err)
}

func TestMultiStore_ListSecrets(t *testing.T) {
	s1 := NewDictStore(map[string]any{"a": 1, "b": 2})
	s2 := NewDictStore(map[string]any{"b": 3, "c": 4})

	multi := NewMultiStore([]confii.SecretStore{s1, s2})
	keys, err := multi.ListSecrets(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, keys, 3) // a, b, c (deduplicated)
}
