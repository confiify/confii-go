// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCommandUsesSelfConfigEnvironmentWhenOmitted(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
default_environment: development
environment_strategy: sectioned
sources:
  - type: yaml
    path: application.yaml
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "application.yaml"), []byte(`
default:
  server:
    port: 8080
development:
  server:
    port: 9090
production:
  server:
    port: 80
`), 0644))
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	out, err := execCobra(NewGetCmd(), []string{"server.port"})
	require.NoError(t, err)
	assert.Equal(t, "9090\n", out)

	out, err = execCobra(NewGetCmd(), []string{"server.port", "--environment", "production"})
	require.NoError(t, err)
	assert.Equal(t, "80\n", out)

	out, err = execCobra(NewGetCmd(), []string{"server"})
	require.NoError(t, err)
	assert.Contains(t, out, `"port": 9090`)

	_, err = execCobra(NewGetCmd(), []string{"missing.key"})
	require.Error(t, err)

	cmd := NewGetCmd()
	cmd.SetOut(errorWriter{})
	cmd.SetArgs([]string{"server.port"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	require.Error(t, cmd.Execute())
}

func TestGetCommandArgumentValidation(t *testing.T) {
	_, err := execCobra(NewGetCmd(), nil)
	require.Error(t, err)
	_, err = execCobra(NewGetCmd(), []string{"one", "two", "three"})
	require.Error(t, err)
	_, err = execCobra(NewGetCmd(), []string{"key", "--loader", "unknown:value"})
	require.Error(t, err)
}
