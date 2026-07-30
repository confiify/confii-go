// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import "testing"

func TestFormatFromExtension(t *testing.T) {
	tests := map[string]Format{
		"config.yaml": FormatYAML,
		"config.YML":  FormatYAML,
		"config.json": FormatJSON,
		"config.toml": FormatTOML,
		"config.ini":  FormatUnknown,
		"config":      FormatUnknown,
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			if got := FormatFromExtension(name); got != want {
				t.Fatalf("FormatFromExtension(%q) = %q, want %q", name, got, want)
			}
		})
	}
}

func TestFormatFromContentType(t *testing.T) {
	tests := map[string]Format{
		"application/json":                FormatJSON,
		"Application/YAML; charset=utf-8": FormatYAML,
		"application/toml":                FormatTOML,
		"text/plain":                      FormatUnknown,
	}
	for contentType, want := range tests {
		t.Run(contentType, func(t *testing.T) {
			if got := FormatFromContentType(contentType); got != want {
				t.Fatalf("FormatFromContentType(%q) = %q, want %q", contentType, got, want)
			}
		})
	}
}
