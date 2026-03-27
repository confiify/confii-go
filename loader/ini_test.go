package loader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestINILoader_Load(t *testing.T) {
	l := NewINI("testdata/simple.ini")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, 5432, db["port"])
}

func TestINILoader_MissingFile(t *testing.T) {
	l := NewINI("testdata/nonexistent.ini")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}
