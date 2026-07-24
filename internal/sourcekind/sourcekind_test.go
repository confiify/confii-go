// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcekind

import "testing"

// TestIsNonFileSource_KnownNonFileSchemes pins the canonical allowlist
// (D-W20-01): every scheme that any of the three pre-Wave-22 lists
// recognized must be classified as non-file by the consolidated predicate.
// Each entry asserts a distinct OBSERVABLE: misclassifying any of these as
// file-backed would re-introduce the drift between watch/sourcetrack/confii
// that this package exists to eliminate.
func TestIsNonFileSource_KnownNonFileSchemes(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"http", "http://example.com/config.yaml"},
		{"https", "https://example.com/config.yaml"},
		{"environment", "environment:APP"},
		{"env_short", "env:DB"},
		{"s3", "s3://bucket/key.yaml"},
		{"ssm", "ssm:/path/to/param"},
		{"gs", "gs://bucket/object"},
		{"azure", "azure://container/blob"},
		{"ibmcos", "ibmcos://bucket/object"},
		{"git", "git:repo@ref:path"},
		{"consul", "consul://kv/path"},
		{"vault_double_slash", "vault://path"},
		{"vault_colon", "vault:path"},
		{"file_remote", "file://example/etc/foo.yaml"},
		// Case-insensitivity: callers may emit upper-case schemes.
		{"https_uppercase", "HTTPS://example.com"},
		{"environment_mixedcase", "Environment:APP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !IsNonFileSource(tc.source) {
				t.Fatalf("IsNonFileSource(%q) = false, want true", tc.source)
			}
		})
	}
}

// TestIsNonFileSource_FilePathInputs asserts the negative observable:
// real filesystem paths (absolute, relative, with/without extension) and
// the empty sentinel must NOT be classified as non-file. Misclassifying a
// real path here would silently disable fsnotify watching and the G14
// incremental Reload optimization.
func TestIsNonFileSource_FilePathInputs(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"empty", ""},
		{"relative_yaml", "config.yaml"},
		{"relative_dot_slash", "./config.yaml"},
		{"relative_subdir", "configs/prod.toml"},
		{"absolute_unix", "/etc/app/config.json"},
		{"absolute_with_dots", "/var/lib/app/.confii.yaml"},
		{"hidden_file", ".confii.yaml"},
		{"relative_parent", "../shared/base.yaml"},
		{"no_extension", "/etc/app/config"},
		{"windows_style", "C:/Users/me/config.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsNonFileSource(tc.source) {
				t.Fatalf("IsNonFileSource(%q) = true, want false", tc.source)
			}
		})
	}
}
