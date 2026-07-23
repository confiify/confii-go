package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/confiify/confii-go/selfconfig"
)

// chdirForHelpers switches CWD to dir, clears the selfconfig cache, and
// registers a cleanup that restores the prior CWD and re-clears the
// cache. Equivalent to the helper used in the top-level builder tests
// but lives here so the cmd-package tests can use it directly.
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

// TestBuildConfig_NoLoaders_AllowsSelfConfigDefaults verifies the G05
// CLI fix: when the user invokes a confii subcommand without -l flags,
// buildConfig must not call WithLoaders([]). Doing so would mark
// "loaders" as explicitly set with zero entries, suppressing
// `default_files` declared in the project's confii.yaml.
func TestBuildConfig_NoLoaders_AllowsSelfConfigDefaults(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "default_files:\n  - selfconfig.yaml\n"
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
		t.Errorf("expected self-config default_files to be honored when no -l flags were supplied")
	}
}

// TestBuildConfig_WithLoaders_TakesPrecedence verifies that when the
// user supplies an explicit -l flag, the loader list is taken as
// authoritative and self-config `default_files` are NOT appended.
// Explicit code wins over self-config (G05).
func TestBuildConfig_WithLoaders_TakesPrecedence(t *testing.T) {
	dir := t.TempDir()

	// Self-config wants to add an extra loader that would, if appended,
	// inject `from_selfconfig: true`.
	confiiYAML := "default_files:\n  - selfconfig.yaml\n"
	if err := os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(confiiYAML), 0644); err != nil {
		t.Fatalf("write confii.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "selfconfig.yaml"), []byte("from_selfconfig: true\n"), 0644); err != nil {
		t.Fatalf("write selfconfig.yaml: %v", err)
	}

	// Caller-supplied YAML — explicit loader argument.
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
		t.Errorf("self-config default_files leaked through despite explicit -l loader")
	}
}
