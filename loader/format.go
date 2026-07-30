// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"path/filepath"
	"strings"
)

// Format identifies a supported serialized configuration format.
type Format string

const (
	// FormatYAML identifies YAML input, including files using a .yml extension.
	FormatYAML Format = "yaml"
	// FormatJSON identifies JSON input.
	FormatJSON Format = "json"
	// FormatTOML identifies TOML input.
	FormatTOML Format = "toml"
	// FormatUnknown requests content detection where the caller supports it.
	FormatUnknown Format = ""
)

// FormatFromExtension detects a configuration format from a file or object
// name. Unknown extensions return FormatUnknown.
func FormatFromExtension(filename string) Format {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".yaml", ".yml":
		return FormatYAML
	case ".json":
		return FormatJSON
	case ".toml":
		return FormatTOML
	default:
		return FormatUnknown
	}
}

// FormatFromContentType detects a configuration format from an HTTP
// Content-Type value. Unrecognized values return FormatUnknown.
func FormatFromContentType(contentType string) Format {
	contentType = strings.ToLower(contentType)
	switch {
	case strings.Contains(contentType, "yaml"):
		return FormatYAML
	case strings.Contains(contentType, "json"):
		return FormatJSON
	case strings.Contains(contentType, "toml"):
		return FormatTOML
	default:
		return FormatUnknown
	}
}
