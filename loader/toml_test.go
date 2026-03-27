package loader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTOMLLoader_Load(t *testing.T) {
	l := NewTOML("testdata/simple.toml")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, int64(5432), db["port"])
}

func TestTOMLLoader_MissingFile(t *testing.T) {
	l := NewTOML("testdata/nonexistent.toml")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}
