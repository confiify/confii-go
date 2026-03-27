package loader

import (
	"context"
	"os"
	"path/filepath"
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

func TestINILoader_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	emptyFile := filepath.Join(dir, "empty.ini")
	require.NoError(t, os.WriteFile(emptyFile, []byte(""), 0644))

	l := NewINI(emptyFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	// An empty INI file has no sections, so result should be nil.
	assert.Nil(t, result)
}
