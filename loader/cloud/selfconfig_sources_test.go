// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestGitSelfConfigSourceRegistration(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSourceProvider("git")
	if !ok {
		t.Fatal("git self-config source provider was not registered")
	}
	loader, err := factory(context.Background(), map[string]any{
		"repository": "https://github.com/confiify/confii-go",
		"file_path":  "config.yaml",
		"branch":     "release",
		"token":      "test-token",
	})
	if err != nil {
		t.Fatalf("build git source: %v", err)
	}
	gitLoader, ok := loader.(*GitLoader)
	if !ok {
		t.Fatalf("loader type = %T, want *GitLoader", loader)
	}
	if gitLoader.branch != "release" || gitLoader.token != "test-token" {
		t.Fatalf("git options were not applied: %#v", gitLoader)
	}
	if _, err := factory(context.Background(), map[string]any{"repository": "https://github.com/confiify/confii-go"}); err == nil {
		t.Fatal("expected missing file_path error")
	}
}

func TestSelfConfigSourceHelpers(t *testing.T) {
	cfg := map[string]any{"primary": " value ", "enabled": "true", "access_key": "key"}
	if got := sourceString(cfg, "missing", "primary"); got != "value" {
		t.Fatalf("sourceString = %q, want value", got)
	}
	if got, err := sourceBool(cfg, "enabled", false); err != nil || !got {
		t.Fatalf("sourceBool = %v, %v", got, err)
	}
	if got, err := sourceBool(cfg, "missing", true); err != nil || !got {
		t.Fatalf("sourceBool fallback = %v, %v", got, err)
	}
	if _, err := sourceBool(map[string]any{"enabled": "invalid"}, "enabled", false); err == nil {
		t.Fatal("expected invalid boolean error")
	}
	if _, _, err := sourceCredentials(cfg); err == nil {
		t.Fatal("expected incomplete credentials error")
	}
}
