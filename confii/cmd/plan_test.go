// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingPlanWriter struct{}

func (failingPlanWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func writePlanProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
default_environment: production
sources:
  - type: environment_files
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "default.yaml"), []byte("server:\n  port: 8080\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "production.yaml"), []byte("server:\n  host: api.example.com\n"), 0o600))
	chdirForHelpers(t, dir)
	return dir
}

func TestPlanCommandText(t *testing.T) {
	writePlanProject(t)
	cmd := NewPlanCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"production"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "Environment: production")
	assert.Contains(t, output.String(), "Strategy: named_files")
	assert.Contains(t, output.String(), "[default]")
	assert.Contains(t, output.String(), "[environment]")
	assert.Contains(t, output.String(), "Mixed environment conflicts: none")
}

func TestPlanCommandJSON(t *testing.T) {
	writePlanProject(t)
	cmd := NewPlanCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--json"})
	require.NoError(t, cmd.Execute())

	var plan confii.SourcePlan
	require.NoError(t, json.Unmarshal(output.Bytes(), &plan))
	assert.Equal(t, confii.EnvironmentStrategyNamedFiles, plan.Strategy)
	require.Len(t, plan.Layers, 2)
	assert.Equal(t, "default", plan.Layers[0].Role)
	assert.Equal(t, "environment", plan.Layers[1].Role)
}

func TestPlanCommandReportsHybridConflicts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "config"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
default_environment: production
environment_strategy: hybrid
environment_conflict_policy: last_wins
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "application.yaml"), []byte("default:\n  server:\n    port: 7000\nproduction:\n  server:\n    host: app.example.com\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "default.yaml"), []byte("server:\n  port: 8080\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "production.yaml"), []byte("server:\n  host: named.example.com\n"), 0o600))
	chdirForHelpers(t, dir)

	cmd := NewPlanCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	require.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "Mixed environment conflicts:")
	assert.Contains(t, output.String(), "server.host")
	assert.Contains(t, output.String(), "server.port")
}

func TestPlanCommandErrors(t *testing.T) {
	t.Run("loader", func(t *testing.T) {
		cmd := NewPlanCmd()
		cmd.SetArgs([]string{"--loader", "unknown:source"})
		require.Error(t, cmd.Execute())
	})

	t.Run("json writer", func(t *testing.T) {
		writePlanProject(t)
		cmd := NewPlanCmd()
		cmd.SetOut(failingPlanWriter{})
		cmd.SetArgs([]string{"--json"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write failed")
	})

	t.Run("text writer", func(t *testing.T) {
		writePlanProject(t)
		cmd := NewPlanCmd()
		cmd.SetOut(failingPlanWriter{})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write failed")
	})
}
