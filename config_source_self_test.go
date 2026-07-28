// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type registeredSourceFixture struct {
	source string
}

type sourceProviderContextKey struct{}

func (l registeredSourceFixture) Load(context.Context) (map[string]any, error) {
	return map[string]any{"registered": true}, nil
}

func (l registeredSourceFixture) Source() string { return l.source }

func TestSelfConfigSourceProviderRegistration(t *testing.T) {
	const provider = "test-registered-source"
	seenContext := false
	RegisterSelfConfigSourceProvider(provider, func(ctx context.Context, cfg map[string]any) (Loader, error) {
		seenContext = ctx.Value(sourceProviderContextKey{}) == "present"
		return registeredSourceFixture{source: cfg["source"].(string)}, nil
	})

	factory, ok := LookupSelfConfigSourceProvider(" TEST-REGISTERED-SOURCE ")
	require.True(t, ok)
	loader, err := factory(context.WithValue(context.Background(), sourceProviderContextKey{}, "present"), map[string]any{"source": "fixture"})
	require.NoError(t, err)
	assert.True(t, seenContext)
	assert.Equal(t, "fixture", loader.Source())

	opts := defaultOptions()
	err = appendSelfConfigSource(context.Background(), &opts, map[string]any{"type": provider, "source": "declarative"})
	require.NoError(t, err)
	require.Len(t, opts.Loaders, 1)
	assert.Equal(t, "declarative", opts.Loaders[0].Source())
}

func TestSelfConfigSourceProviderBuildFailures(t *testing.T) {
	RegisterSelfConfigSourceProvider("test-source-error", func(context.Context, map[string]any) (Loader, error) {
		return nil, errors.New("factory failed")
	})
	RegisterSelfConfigSourceProvider("test-source-nil", func(context.Context, map[string]any) (Loader, error) {
		return nil, nil
	})

	_, err := buildRegisteredSelfConfigSource(context.Background(), "test-source-error", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.Contains(t, err.Error(), "factory failed")

	_, err = buildRegisteredSelfConfigSource(context.Background(), "test-source-nil", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.Contains(t, err.Error(), "nil loader")

	RegisterSelfConfigSourceProvider("ignored-nil", nil)
	_, ok := LookupSelfConfigSourceProvider("ignored-nil")
	assert.False(t, ok)
}

func TestSecretReferenceKeysAndProviderAreValueSafe(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
sources:
  - type: yaml
    path: `+filepath.Join(dir, "app.yaml")+`
secrets:
  provider: dict
  entries:
    database-password: do-not-return
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
database:
  password: ${secret:database-password}
tokens:
  - literal
  - ${secret:database-password}
plain: value
`), 0o600))

	cfg, err := New[any](context.Background(), WithWorkingDir(dir))
	require.NoError(t, err)
	assert.Equal(t, "dict", cfg.SecretProvider())
	assert.Equal(t, []string{"database.password", "tokens"}, cfg.SecretReferenceKeys())
}
