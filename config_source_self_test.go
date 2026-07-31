// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eagerCountingSecretStore struct {
	mu    sync.Mutex
	calls int
}

func (s *eagerCountingSecretStore) ReadSecret(context.Context, SecretRequest) (any, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return map[string]any{"username": "demo", "password": "resolved"}, nil
}

func (s *eagerCountingSecretStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestDeclarativeSecretsAreEagerAndDeduplicated(t *testing.T) {
	dir := t.TempDir()
	store := &eagerCountingSecretStore{}
	RegisterSelfConfigSecretProvider("test-eager-counting", func(context.Context, map[string]any) (SecretReader, error) {
		return store, nil
	})
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
sources:
  - type: yaml
    path: `+filepath.Join(dir, "app.yaml")+`
secrets:
  default_provider: test
  providers:
    test:
      type: test-eager-counting
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
database:
  username: ${secret:database-credentials:username}
  password: ${secret:database-credentials:password}
`), 0o600))

	cfg, err := NewWithContext[any](context.Background(), WithWorkingDir(dir))
	require.NoError(t, err)
	assert.Equal(t, 1, store.callCount(), "one remote document must serve both JSON paths")
	assert.Equal(t, "demo", cfg.GetStringOr("database.username", ""))
	assert.Equal(t, "resolved", cfg.GetStringOr("database.password", ""))
	assert.Equal(t, 1, store.callCount(), "ordinary reads must use the materialized snapshot")
	assert.Equal(t, []string{"database.password", "database.username"}, cfg.SecretReferenceKeys())
	assert.Equal(t, redactedSecretValue, cfg.Explain("database.password")["current_value"])
	assert.Equal(t, redactedSecretValue, cfg.Schema("database.password")["value"])
	docs, docsErr := cfg.GenerateDocs("json")
	require.NoError(t, docsErr)
	assert.NotContains(t, docs, "resolved")

	require.NoError(t, cfg.RefreshSecretsWithContext(context.Background()))
	assert.Equal(t, 2, store.callCount(), "an explicit refresh starts a new deduplicated provider session")
}

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

	assert.Panics(t, func() { RegisterSelfConfigSourceProvider("ignored-nil", nil) })
	_, ok := LookupSelfConfigSourceProvider("ignored-nil")
	assert.False(t, ok)
	assert.Panics(t, func() {
		RegisterSelfConfigSourceProvider("", func(context.Context, map[string]any) (Loader, error) { return nil, nil })
	})
	name := "test-source-duplicate-registration"
	factory := func(context.Context, map[string]any) (Loader, error) { return nil, nil }
	RegisterSelfConfigSourceProvider(name, factory)
	assert.Panics(t, func() {
		RegisterSelfConfigSourceProvider("  TEST-SOURCE-DUPLICATE-REGISTRATION  ", factory)
	})
}

func TestSecretReferenceKeysAndProviderAreValueSafe(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
sources:
  - type: yaml
    path: `+filepath.Join(dir, "app.yaml")+`
secrets:
  default_provider: local
  providers:
    local:
      type: dict
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

	cfg, err := NewWithContext[any](context.Background(), WithWorkingDir(dir))
	require.NoError(t, err)
	assert.Equal(t, "local", cfg.SecretProvider())
	assert.Equal(t, []string{"local"}, cfg.SecretProviders())
	assert.Equal(t, []string{"local"}, cfg.SecretReferenceProviders())
	assert.Equal(t, []string{"database.password", "tokens"}, cfg.SecretReferenceKeys())
}

func TestNamedSecretProvidersUseEnvironmentDefaultAndExplicitRouting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
default_environment: development
sources:
  - type: yaml
    path: `+filepath.Join(dir, "app.yaml")+`
secrets:
  default_provider: development
  environment_defaults:
    production: production
  providers:
    development:
      type: dict
      entries:
        database-password: development-password
    production:
      type: dict
      entries:
        database-password: production-password
    shared:
      type: dict
      entries:
        signing-key: shared-signing-key
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
database:
  password: ${secret:database-password}
security:
  signing_key: ${secret@shared:signing-key}
`), 0o600))

	cfg, err := NewWithContext[any](context.Background(), WithWorkingDir(dir), WithEnv("production"))
	require.NoError(t, err)
	assert.Equal(t, "production", cfg.SecretProvider())
	assert.Equal(t, []string{"development", "production", "shared"}, cfg.SecretProviders())
	assert.Equal(t, []string{"production", "shared"}, cfg.SecretReferenceProviders())
	assert.Equal(t, []string{"production"}, cfg.SecretReferenceProvidersFor("database"))
	assert.Equal(t, []string{"shared"}, cfg.SecretReferenceProvidersFor("security.signing_key"))
	assert.Empty(t, cfg.SecretReferenceProvidersFor("missing"))
	assert.Equal(t, "production-password", mustGet(t, cfg, "database.password"))
	assert.Equal(t, "shared-signing-key", mustGet(t, cfg, "security.signing_key"))
}

func mustGet(t *testing.T, cfg *Config[any], key string) any {
	t.Helper()
	value, err := cfg.GetWithContext(context.Background(), key)
	require.NoError(t, err)
	return value
}
