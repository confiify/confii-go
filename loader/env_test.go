package loader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentLoader_Load(t *testing.T) {
	t.Setenv("TESTAPP_DATABASE__HOST", "localhost")
	t.Setenv("TESTAPP_DATABASE__PORT", "5432")
	t.Setenv("TESTAPP_DEBUG", "true")

	l := NewEnvironment("TESTAPP")
	assert.Equal(t, "environment:TESTAPP", l.Source())

	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, 5432, db["port"])
	assert.Equal(t, true, result["debug"])
}

func TestEnvironmentLoader_NoMatches(t *testing.T) {
	l := NewEnvironment("NOMATCH_ZZZZZ")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnvironmentLoader_EnvVarWithEmptyValue(t *testing.T) {
	// An env var with no value after = should still be picked up.
	t.Setenv("EMPTYVAL_KEY", "")

	l := NewEnvironment("EMPTYVAL")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "", result["key"])
}

func TestEnvironmentLoader_CustomSeparator(t *testing.T) {
	t.Setenv("MYAPP_DB_HOST", "dbhost")

	l := NewEnvironment("MYAPP", WithSeparator("_"))
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["db"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "dbhost", db["host"])
}
