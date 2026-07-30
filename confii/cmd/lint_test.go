// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"
)

func TestLintCommand_NoIssues(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeTestFile(t, dir, "ok.yaml", "host: localhost\nport: 5432\n")

	cmd := NewLintCmd()
	out, err := execCobra(cmd, []string{"-l", "yaml:" + yamlPath})
	if err != nil {
		t.Fatalf("expected nil error, got %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "No issues found.") {
		t.Errorf("expected 'No issues found.', got: %s", out)
	}
}

func TestLintCommand_ReturnsErrorInsteadOfExit(t *testing.T) {
	dir := t.TempDir()

	yamlPath := writeTestFile(t, dir, "empty.yaml", "")

	cmd := NewLintCmd()
	_, err := execCobra(cmd, []string{"-l", "yaml:" + yamlPath, "--strict"})
	if err == nil {
		t.Fatalf("expected --strict to return an error when issues are found, got nil")
	}
	if !strings.Contains(err.Error(), "lint found") {
		t.Errorf("expected error to mention 'lint found', got: %v", err)
	}
}

func TestLintCommand_NonStrict_DoesNotError(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeTestFile(t, dir, "empty.yaml", "")

	cmd := NewLintCmd()
	out, err := execCobra(cmd, []string{"-l", "yaml:" + yamlPath})
	if err != nil {
		t.Fatalf("expected nil error in non-strict mode, got %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "issue(s) found") {
		t.Errorf("expected issue report in output, got: %s", out)
	}
}
