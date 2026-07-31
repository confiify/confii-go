// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package formatparse detects configuration file formats from file extensions
// and content types.
package formatparse

import (
	"mime"
	"path/filepath"
	"strings"
)

// Format represents a configuration file format.
type Format string

const (
	// FormatYAML identifies YAML input, including .yml files.
	FormatYAML Format = "yaml"
	// FormatJSON identifies JSON input.
	FormatJSON Format = "json"
	// FormatTOML identifies TOML input.
	FormatTOML Format = "toml"
	// FormatINI identifies INI input, including .cfg files.
	FormatINI Format = "ini"
	// FormatEnvFile identifies dotenv input.
	FormatEnvFile Format = "env"
	// FormatUnknown indicates that no supported format was recognized.
	FormatUnknown Format = ""
)

// FromExtension detects format from a file extension.
func FromExtension(filename string) Format {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".yaml", ".yml":
		return FormatYAML
	case ".json":
		return FormatJSON
	case ".toml":
		return FormatTOML
	case ".ini", ".cfg":
		return FormatINI
	case ".env":
		return FormatEnvFile
	default:
		return FormatUnknown
	}
}

// FromContentType detects format from an HTTP Content-Type header value.
func FromContentType(ct string) Format {
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return FormatUnknown
	}
	mediaType = strings.ToLower(mediaType)
	switch mediaType {
	case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml":
		return FormatYAML
	case "application/json", "text/json":
		return FormatJSON
	case "application/toml", "application/x-toml", "text/toml":
		return FormatTOML
	}
	if strings.HasSuffix(mediaType, "+yaml") {
		return FormatYAML
	}
	if strings.HasSuffix(mediaType, "+json") {
		return FormatJSON
	}
	return FormatUnknown
}
