package loader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvFileLoader_Load(t *testing.T) {
	l := NewEnvFile("testdata/simple.env")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "localhost", result["HOST"])
	assert.Equal(t, 5432, result["PORT"])
	assert.Equal(t, true, result["DEBUG"])
	assert.Equal(t, "my app", result["NAME"])         // double-quoted
	assert.Equal(t, "raw_value", result["SECRET"])     // single-quoted
	assert.Equal(t, "some_value", result["INLINE"])    // inline comment stripped

	// Nested key via dot notation.
	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "db-server", db["host"])
}

func TestEnvFileLoader_MissingFile(t *testing.T) {
	l := NewEnvFile("testdata/nonexistent.env")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnvFileLoader_DefaultPath(t *testing.T) {
	l := NewEnvFile("")
	assert.Equal(t, ".env", l.Source())
}
