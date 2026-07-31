// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"github.com/confiify/confii-go/v2/internal/formatparse"
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
	switch formatparse.FromExtension(filename) {
	case formatparse.FormatYAML:
		return FormatYAML
	case formatparse.FormatJSON:
		return FormatJSON
	case formatparse.FormatTOML:
		return FormatTOML
	default:
		return FormatUnknown
	}
}

// FormatFromContentType detects a configuration format from an HTTP
// Content-Type value. Unrecognized values return FormatUnknown.
func FormatFromContentType(contentType string) Format {
	switch formatparse.FromContentType(contentType) {
	case formatparse.FormatYAML:
		return FormatYAML
	case formatparse.FormatJSON:
		return FormatJSON
	case formatparse.FormatTOML:
		return FormatTOML
	default:
		return FormatUnknown
	}
}
