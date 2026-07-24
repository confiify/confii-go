// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/BurntSushi/toml"
	confii "github.com/confiify/confii-go"
)

// TOMLLoader loads configuration from a TOML file.
//
// Absence of the configured file is governed by the loader's
// [confii.ErrorPolicy] (default [confii.ErrorPolicyRaise]) (G07):
//
//   - ErrorPolicyRaise:  Load returns a typed [*confii.ConfigError]
//     wrapping [confii.ErrConfigLoad].
//   - ErrorPolicyWarn:   the missing file is logged via the configured
//     [*slog.Logger] and Load returns (nil, nil).
//   - ErrorPolicyIgnore: the missing file is silently skipped (legacy
//     pre-G07 behavior).
type TOMLLoader struct {
	source      string
	errorPolicy confii.ErrorPolicy
	logger      *slog.Logger
}

// TOMLOption configures the TOMLLoader.
type TOMLOption func(*TOMLLoader)

// WithTOMLErrorPolicy sets how the loader reacts to load-time failures
// (missing file, I/O error, malformed TOML). Defaults to
// [confii.ErrorPolicyRaise].
func WithTOMLErrorPolicy(p confii.ErrorPolicy) TOMLOption {
	return func(l *TOMLLoader) { l.errorPolicy = p }
}

// WithTOMLLogger sets the logger used when the error policy is
// [confii.ErrorPolicyWarn]. Nil is ignored. Defaults to [slog.Default].
func WithTOMLLogger(logger *slog.Logger) TOMLOption {
	return func(l *TOMLLoader) {
		if logger != nil {
			l.logger = logger
		}
	}
}

// NewTOML creates a new TOML loader for the given file path. The loader's
// error policy defaults to [confii.ErrorPolicyRaise]; pass
// [WithTOMLErrorPolicy] to change it for genuinely optional files.
func NewTOML(path string, opts ...TOMLOption) *TOMLLoader {
	l := &TOMLLoader{
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
func (l *TOMLLoader) Source() string { return l.source }

// Load reads and parses the TOML file at the configured path, returning
// the parsed configuration as a map. Failures are dispatched through the
// loader's [confii.ErrorPolicy]; see [TOMLLoader] for details.
func (l *TOMLLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return l.handleMissing(err)
		}
		return nil, confii.NewLoadError(l.source, err)
	}

	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		return nil, confii.NewFormatError(l.source, "toml", err)
	}
	return result, nil
}

// handleMissing dispatches an os.ErrNotExist condition through the
// configured error policy.
func (l *TOMLLoader) handleMissing(err error) (map[string]any, error) {
	switch l.errorPolicy {
	case confii.ErrorPolicyIgnore:
		return nil, nil
	case confii.ErrorPolicyWarn:
		l.logger.Warn(
			"toml: source file missing",
			slog.String("source", l.source),
		)
		return nil, nil
	default:
		return nil, confii.NewLoadError(l.source, err)
	}
}
