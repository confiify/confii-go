package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// writeTestFile is a small helper: writes content to <dir>/<name> and
// fails the test on I/O error. Returns the absolute path.
func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// execCobra runs a Cobra command with the given args, capturing
// stdout+stderr into a single buffer. If the command's RunE called
// os.Exit, the entire test process would terminate before the
// assertion in the caller — so the fact that this helper returns at
// all is part of the assertion that os.Exit has been removed.
func execCobra(cmd *cobra.Command, args []string) (string, error) {
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	// Silence Cobra's own usage/error printing so the buffer reflects
	// only what the command itself wrote.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	return buf.String(), err
}

// validSchema is a tiny JSON Schema requiring a string "host" key.
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
	// "host" is required by schema; this YAML omits it.
	yamlPath := writeTestFile(t, dir, "bad.yaml", "port: 5432\n")

	cmd := NewValidateCmd()
	// If validate.go still called os.Exit(1) on validation failure,
	// the test binary would terminate here with exit code 1 and the
	// `t.Fatalf` below would never run — the test would simply be
	// reported as "FAIL: process exited". Reaching the assertion is
	// itself part of what's being verified.
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
	cmd := NewValidateCmd()
	_, err := execCobra(cmd, []string{})
	if err == nil {
		t.Fatalf("expected error when --schema is missing, got nil")
	}
	if !strings.Contains(err.Error(), "--schema") {
		t.Errorf("expected error to mention --schema, got: %v", err)
	}
}
