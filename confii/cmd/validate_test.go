// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func execCobra(cmd *cobra.Command, args []string) (string, error) {
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	return buf.String(), err
}

const validSchema = `{
	"type": "object",
	"properties": {
		"host": {"type": "string"},
		"port": {"type": "integer"}
	},
	"required": ["host"]
}`

func TestValidateCommand_HappyPath(t *testing.T) {
	dir := t.TempDir()
	schemaPath := writeTestFile(t, dir, "schema.json", validSchema)
	yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\nport: 5432\n")

	cmd := NewValidateCmd()
	out, err := execCobra(cmd, []string{
		"production",
		"--schema", schemaPath,
		"-l", "yaml:" + yamlPath,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Configuration is valid.") {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestValidateCommand_ReturnsErrorInsteadOfExit(t *testing.T) {
	dir := t.TempDir()
	schemaPath := writeTestFile(t, dir, "schema.json", validSchema)

	yamlPath := writeTestFile(t, dir, "bad.yaml", "port: 5432\n")

	cmd := NewValidateCmd()

	_, err := execCobra(cmd, []string{
		"--schema", schemaPath,
		"-l", "yaml:" + yamlPath,
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("expected wrapped validation-failed error, got: %v", err)
	}
}

func TestValidateCommand_MissingSchemaFlag_ReturnsError(t *testing.T) {
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)
	cmd := NewValidateCmd()
	_, err := execCobra(cmd, []string{})
	if err == nil {
		t.Fatalf("expected error when --schema is missing, got nil")
	}
	if !strings.Contains(err.Error(), "--schema") {
		t.Errorf("expected error to mention --schema, got: %v", err)
	}
}

func TestValidateCommandUsesSelfConfigSchemaPath(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "schema.json", validSchema)
	writeTestFile(t, dir, "config.yaml", "host: localhost\nport: 5432\n")
	writeTestFile(t, dir, ".confii.yaml", `
schema_path: schema.json
sources:
  - type: yaml
    path: config.yaml
`)
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	out, err := execCobra(NewValidateCmd(), nil)
	require.NoError(t, err)
	if !strings.Contains(out, "Configuration is valid.") {
		t.Fatalf("expected success message, got %q", out)
	}
}

func TestValidateCommandFlagOverridesSelfConfigSchemaPath(t *testing.T) {
	dir := t.TempDir()
	goodSchema := writeTestFile(t, dir, "good-schema.json", validSchema)
	writeTestFile(t, dir, "bad-schema.json", "not json")
	yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\n")
	writeTestFile(t, dir, ".confii.yaml", "schema_path: bad-schema.json\n")
	withWorkingDirectory(t, dir)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	_, err := execCobra(NewValidateCmd(), []string{"--schema", goodSchema, "-l", "yaml:" + yamlPath})
	require.NoError(t, err)
}

func TestValidateCommandReportsSelfConfigAndOutputErrors(t *testing.T) {
	t.Run("malformed self-config", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, dir, ".confii.yaml", "schema_path: [\n")
		withWorkingDirectory(t, dir)
		selfconfig.ClearCache()
		t.Cleanup(selfconfig.ClearCache)

		_, err := execCobra(NewValidateCmd(), nil)
		require.Error(t, err)
		if !strings.Contains(err.Error(), "read self-config schema_path") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("config construction", func(t *testing.T) {
		dir := t.TempDir()
		schemaPath := writeTestFile(t, dir, "schema.json", validSchema)
		_, err := execCobra(NewValidateCmd(), []string{"--schema", schemaPath, "--loader", "unknown:value"})
		require.Error(t, err)
	})

	t.Run("schema loading", func(t *testing.T) {
		dir := t.TempDir()
		badSchema := writeTestFile(t, dir, "schema.json", "not json")
		_, err := execCobra(NewValidateCmd(), []string{"--schema", badSchema})
		require.Error(t, err)
		if !strings.Contains(err.Error(), "load schema") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success output", func(t *testing.T) {
		dir := t.TempDir()
		schemaPath := writeTestFile(t, dir, "schema.json", validSchema)
		yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\n")
		cmd := NewValidateCmd()
		cmd.SetOut(errorWriter{})
		cmd.SetArgs([]string{"--schema", schemaPath, "--loader", "yaml:" + yamlPath})
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		require.Error(t, cmd.Execute())
	})
}
