// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"errors"
	"log/slog"
	"os"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/formatparse"
	"github.com/confiify/confii-go/v2/internal/typecoerce"
	"gopkg.in/ini.v1"
)

// INILoader loads configuration from an INI file.
// Each section becomes a top-level key; keys within sections are nested.
//
// Keys that appear before any
// `[section]` header — including the `DEFAULT` pseudo-section produced
// by `gopkg.in/ini.v1` for unsectioned key/value pairs — are promoted
// to root-level keys of the returned map. This supports INI files such as:
//
//	host = localhost
//	port = 5432
//
// If a root key shares a name with a sibling section, the section wins
// because sections are parsed after root keys.
//
// Absence of the configured file is governed by the loader's
// [confii.ErrorPolicy] (default [confii.ErrorPolicyRaise]):
//
//   - ErrorPolicyRaise:  Load returns a typed [*confii.ConfigError]
//     wrapping [confii.ErrConfigLoad].
//   - ErrorPolicyWarn:   the missing file is logged via the configured
//     [*slog.Logger] and Load returns (nil, nil).
//   - ErrorPolicyIgnore: the missing file is silently skipped.
type INILoader struct {
	source      string
	errorPolicy confii.ErrorPolicy
	logger      *slog.Logger
}

// INIOption configures the INILoader.
type INIOption func(*INILoader)

// WithINIErrorPolicy sets how the loader reacts to load-time failures
// (missing file, I/O error, malformed INI). Defaults to
// [confii.ErrorPolicyRaise].
func WithINIErrorPolicy(p confii.ErrorPolicy) INIOption {
	return func(l *INILoader) { l.errorPolicy = p }
}

// WithINILogger sets the logger used when the error policy is
// [confii.ErrorPolicyWarn]. Nil is ignored. Defaults to [slog.Default()].
func WithINILogger(logger *slog.Logger) INIOption {
	return func(l *INILoader) {
		if logger != nil {
			l.logger = logger
		}
	}
}

// NewINI creates a new INI loader for the given file path. The loader's
// error policy defaults to [confii.ErrorPolicyRaise]; pass
// [WithINIErrorPolicy] to change it for genuinely optional files.
//
// Top-level key/value pairs that appear before any `[section]` header
// are surfaced as root-level keys of the returned configuration map;
// see [INILoader] for the full defaults-only convention.
func NewINI(path string, opts ...INIOption) *INILoader {
	l := &INILoader{
		source:      path,
		errorPolicy: confii.ErrorPolicyRaise,
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Source returns the identifier for this loader's configuration source.
func (l *INILoader) Source() string { return l.source }

// Load reads and parses the INI file at the configured path, returning
// sections as nested configuration maps. Failures are dispatched through
// the loader's [confii.ErrorPolicy]; see [INILoader] for details.
func (l *INILoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return l.handleMissing(err)
		}
		return nil, confii.NewLoadError(l.source, err)
	}
	if err := formatparse.ValidateDeclaredContent(formatparse.FormatINI, data); err != nil {
		return nil, confii.NewFormatError(l.source, "ini", err)
	}

	cfg, err := ini.Load(data)
	if err != nil {
		return nil, confii.NewFormatError(l.source, "ini", err)
	}

	result := make(map[string]any)
	for _, section := range cfg.Sections() {
		name := section.Name()
		// The gopkg.in/ini.v1 library reports a synthetic
		// "DEFAULT" section that holds every key/value pair preceding
		// the first explicit [section] header. Surface those keys as
		// root-level entries of the result map instead of dropping
		// them, so defaults-only INI files (and the leading block of
		// mixed files) round-trip into the configuration.
		if name == ini.DefaultSection {
			for _, key := range section.Keys() {
				result[key.Name()] = typecoerce.ParseScalar(key.Value(), false)
			}
			continue
		}
		sectionMap := make(map[string]any)
		for _, key := range section.Keys() {
			sectionMap[key.Name()] = typecoerce.ParseScalar(key.Value(), false)
		}
		result[name] = sectionMap
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// handleMissing dispatches an os.ErrNotExist condition through the
// configured error policy.
func (l *INILoader) handleMissing(err error) (map[string]any, error) {
	switch l.errorPolicy {
	case confii.ErrorPolicyIgnore:
		return nil, nil
	case confii.ErrorPolicyWarn:
		l.logger.Warn(
			"ini: source file missing",
			slog.String("source", l.source),
		)
		return nil, nil
	default:
		return nil, confii.NewLoadError(l.source, err)
	}
}
