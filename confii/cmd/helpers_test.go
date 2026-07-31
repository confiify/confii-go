// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/confiify/confii-go/v2/selfconfig"
)

func TestCreateLoaderUsesCanonicalTypeNames(t *testing.T) {
	for _, tt := range []struct {
		typeName string
		source   string
	}{
		{"yaml", "config.yml"},
		{"json", "config.json"},
		{"toml", "config.toml"},
		{"ini", "config.cfg"},
		{"dotenv", ".env.local"},
		{"environment", "APP"},
	} {
		t.Run(tt.typeName, func(t *testing.T) {
			got, err := createLoader(tt.typeName, tt.source)
			if err != nil || got == nil {
				t.Fatalf("createLoader(%q) = %v, %v", tt.typeName, got, err)
			}
		})
	}

	for _, alias := range []string{"yml", "cfg", "env", "envfile", "env_file", "env-vars"} {
		t.Run("reject/"+alias, func(t *testing.T) {
			_, err := createLoader(alias, "value")
			if err == nil || !strings.Contains(err.Error(), "unknown loader type") {
				t.Fatalf("createLoader(%q) error = %v", alias, err)
			}
		})
	}
}

func chdirForHelpers(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
		selfconfig.ClearCache()
	})
	selfconfig.ClearCache()
}

func TestBuildConfig_NoLoaders_AllowsSelfConfigDefaults(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "sources:\n  - type: yaml\n    path: selfconfig.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(confiiYAML), 0644); err != nil {
		t.Fatalf("write confii.yaml: %v", err)
	}
	selfYAML := "from_selfconfig: true\nshared: from_selfconfig\n"
	if err := os.WriteFile(filepath.Join(dir, "selfconfig.yaml"), []byte(selfYAML), 0644); err != nil {
		t.Fatalf("write selfconfig.yaml: %v", err)
	}

	chdirForHelpers(t, dir)

	cfg, err := buildConfig("", nil)
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}

	if !cfg.Has("from_selfconfig") {
		t.Errorf("expected self-config sources to be honored when no -l flags were supplied")
	}
}

func TestBuildConfig_WithLoaders_TakesPrecedence(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "sources:\n  - type: yaml\n    path: selfconfig.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(confiiYAML), 0644); err != nil {
		t.Fatalf("write confii.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "selfconfig.yaml"), []byte("from_selfconfig: true\n"), 0644); err != nil {
		t.Fatalf("write selfconfig.yaml: %v", err)
	}

	explicitPath := filepath.Join(dir, "explicit.yaml")
	if err := os.WriteFile(explicitPath, []byte("from_explicit: true\n"), 0644); err != nil {
		t.Fatalf("write explicit.yaml: %v", err)
	}

	chdirForHelpers(t, dir)

	cfg, err := buildConfig("", []string{"yaml:" + explicitPath})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}

	if !cfg.Has("from_explicit") {
		t.Errorf("expected explicit loader's keys to be present")
	}
	if cfg.Has("from_selfconfig") {
		t.Errorf("self-config sources leaked through despite explicit -l loader")
	}
}
