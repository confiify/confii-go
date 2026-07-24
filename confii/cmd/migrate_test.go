// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"
)

func TestMigrateCommand_DispatchesOnSourceType(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\nport: 5432\n")
	envPath := writeTestFile(t, dir, "config.env", "HOST=localhost\nPORT=5432\n")
	tomlPath := writeTestFile(t, dir, "settings.toml", "host = \"localhost\"\nport = 5432\n")

	tests := []struct {
		name       string
		sourceType string
		configFile string
		wantErr    bool
		errSubstr  string
	}{
		{name: "yaml dispatches to YAML loader", sourceType: "yaml", configFile: yamlPath},
		{name: "yml dispatches to YAML loader", sourceType: "yml", configFile: yamlPath},
		{name: "dotenv dispatches to env-file loader", sourceType: "dotenv", configFile: envPath},
		{name: "auto preserves legacy YAML-then-env-file detection", sourceType: "auto", configFile: yamlPath},
		{name: "empty source type also auto-detects", sourceType: "", configFile: yamlPath},
		{name: "dynaconf imports YAML settings", sourceType: "dynaconf", configFile: yamlPath},
		{name: "dynaconf imports TOML settings", sourceType: "dynaconf", configFile: tomlPath},
		{name: "hydra imports materialized YAML", sourceType: "hydra", configFile: yamlPath},
		{name: "omegaconf imports materialized YAML", sourceType: "omegaconf", configFile: yamlPath},
		{
			name:       "unknown source type returns clear error",
			sourceType: "no-such-source", configFile: yamlPath,
			wantErr:   true,
			errSubstr: "unknown source type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadMigrateSource(tc.sourceType, tc.configFile)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (cfg=%v)", cfg)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("expected error containing %q, got: %v", tc.errSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if cfg == nil {
				t.Fatalf("expected non-nil config")
			}
		})
	}
}

func TestMigrateCommand_NoSourceType_AutoDetects(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\nport: 5432\n")

	// Empty source type must preserve legacy auto-detect (YAML works).
	cfg, err := loadMigrateSource("", yamlPath)
	if err != nil {
		t.Fatalf("auto-detect should succeed for YAML, got %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}

	// And auto-detect should also fall back to .env.
	envPath := writeTestFile(t, dir, "config.env", "HOST=localhost\n")
	cfg, err = loadMigrateSource("", envPath)
	if err != nil {
		t.Fatalf("auto-detect should fall back to env-file, got %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config from env-file fallback")
	}
}

func TestMigrateCommand_PositionalArgsExecute(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\nport: 5432\n")

	// Two-arg form: <source-type> <config-file>.
	cmd := NewMigrateCmd()
	out, err := execCobra(cmd, []string{
		"yaml", yamlPath,
		"--target-format", "json",
	})
	if err != nil {
		t.Fatalf("migrate yaml->json failed: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "localhost") {
		t.Errorf("expected exported config to contain host value, got: %s", out)
	}
}

func TestMigrateCommand_HydraViaCobra(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\n")

	cmd := NewMigrateCmd()
	out, err := execCobra(cmd, []string{"hydra", yamlPath, "--target-format", "json"})
	if err != nil {
		t.Fatalf("migrate hydra failed: %v", err)
	}
	if !strings.Contains(out, "localhost") {
		t.Errorf("expected migrated value, got: %s", out)
	}
}

func TestMigrateCommand_FlagAndPositional_Conflict(t *testing.T) {
	dir := t.TempDir()
	yamlPath := writeTestFile(t, dir, "config.yaml", "host: localhost\n")

	cmd := NewMigrateCmd()
	_, err := execCobra(cmd, []string{
		"--source-type", "dotenv",
		"yaml", yamlPath,
	})
	if err == nil {
		t.Fatalf("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "conflicts") {
		t.Errorf("expected conflict error, got: %v", err)
	}
}
