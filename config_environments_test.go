// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAvailableEnvironmentsNamedFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "settings"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte(`
default_environment: development
environment_strategy: named_files
sources:
  - type: environment_files
    search_paths: [settings]
    default_file: shared.yml
    environment_file: app-{environment}.yml
`), 0o600))
	for _, name := range []string{"shared.yml", "app-development.yml", "app-production.yml", "unrelated.yml"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "settings", name), []byte("app:\n  ready: true\n"), 0o600))
	}

	cfg, err := confii.NewWithContext[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	environments, err := cfg.AvailableEnvironments()
	require.NoError(t, err)
	assert.Equal(t, []string{"development", "production"}, environments)
}

func TestAvailableEnvironmentsSectioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
default:
  server:
    port: 8080
development:
  log:
    level: debug
production:
  server:
    host: api.example.com
metadata: ordinary-scalar
`), 0o600))
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnv("development"),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	environments, err := cfg.AvailableEnvironments()
	require.NoError(t, err)
	assert.Equal(t, []string{"development", "production"}, environments)
}

func TestAvailableEnvironmentsReturnsIndependentSlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.yaml")
	require.NoError(t, os.WriteFile(path, []byte("default: {}\ndevelopment: {}\n"), 0o600))
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnv("development"),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	first, err := cfg.AvailableEnvironments()
	require.NoError(t, err)
	first[0] = "changed"
	second, err := cfg.AvailableEnvironments()
	require.NoError(t, err)
	assert.Equal(t, []string{"development"}, second)
}
