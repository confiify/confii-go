// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package loader provides implementations of the confii.Loader interface
// for various configuration sources.
package loader

import (
	"context"
	"errors"
	"log/slog"
	"os"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/configdecode"
	"github.com/confiify/confii-go/v2/internal/formatparse"
)

// YAMLLoader loads configuration from a YAML file.
//
// Absence of the configured file is governed by the loader's
// [confii.ErrorPolicy] (default [confii.ErrorPolicyRaise]) just like any
// other failure mode:
//
//   - ErrorPolicyRaise:  Load returns a typed [*confii.ConfigError]
//     wrapping [confii.ErrConfigLoad]. Callers that genuinely treat the
//     file as optional must opt in via [WithYAMLErrorPolicy].
//   - ErrorPolicyWarn:   the missing file is logged via the configured
//     [*slog.Logger] and Load returns (nil, nil).
//   - ErrorPolicyIgnore: the missing file is silently skipped.
//
// Other I/O errors (permission denied, malformed YAML, etc.) are also
// dispatched through the policy.
//
// Key normalization: YAML maps with non-string keys (integers,
// booleans, sequences, etc.) decode under go.yaml.in/yaml/v3 as
// map[interface{}]interface{}, which is incompatible with the rest of
// the library's map[string]any data model (it breaks dot-path access,
// JSON export, and typed configuration decoding). The loader walks the
// unmarshaled tree and coerces every map key to a string via
// fmt.Sprint, recursively descending into nested maps and slices, so
// the returned value is always a fully string-keyed map[string]any.
type YAMLLoader struct {
	source      string
	errorPolicy confii.ErrorPolicy
	logger      *slog.Logger
}

// YAMLOption configures the YAMLLoader.
type YAMLOption func(*YAMLLoader)

// WithYAMLErrorPolicy sets how the loader reacts to load-time failures
// (missing file, I/O error, malformed YAML). Defaults to
// [confii.ErrorPolicyRaise].
func WithYAMLErrorPolicy(p confii.ErrorPolicy) YAMLOption {
	return func(l *YAMLLoader) { l.errorPolicy = p }
}

// WithYAMLLogger sets the logger used when the error policy is
// [confii.ErrorPolicyWarn]. Nil is ignored. Defaults to [slog.Default()].
func WithYAMLLogger(logger *slog.Logger) YAMLOption {
	return func(l *YAMLLoader) {
		if logger != nil {
			l.logger = logger
		}
	}
}

// NewYAML creates a new YAML loader for the given file path. The loader's
// error policy defaults to [confii.ErrorPolicyRaise]; pass
// [WithYAMLErrorPolicy] to change it for genuinely optional files.
func NewYAML(path string, opts ...YAMLOption) *YAMLLoader {
	l := &YAMLLoader{
		source:      path,
		errorPolicy: confii.ErrorPolicyRaise,
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Source returns the configured path exactly as supplied to NewYAML.
func (l *YAMLLoader) Source() string { return l.source }

// Load reads one YAML mapping from the configured path. Top-level scalars and
// sequences, cross-format content, and key-normalization collisions wrap
// [confii.ErrConfigFormat]. File errors wrap [confii.ErrConfigLoad], subject to
// the configured missing-file policy. Local reads are synchronous; ctx is
// accepted for Loader compatibility but is not observed.
func (l *YAMLLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return l.handleMissing(err)
		}
		return nil, confii.NewLoadError(l.source, err)
	}
	result, err := configdecode.Map(data, formatparse.FormatYAML)
	if err != nil {
		return nil, confii.NewFormatError(l.source, "yaml", err)
	}
	return result, nil
}

// handleMissing dispatches an os.ErrNotExist condition through the
// configured error policy. Raise returns a typed *confii.ConfigError;
// Warn logs and returns (nil, nil); Ignore returns (nil, nil) silently.
func (l *YAMLLoader) handleMissing(err error) (map[string]any, error) {
	switch l.errorPolicy {
	case confii.ErrorPolicyIgnore:
		return nil, nil
	case confii.ErrorPolicyWarn:
		l.logger.Warn(
			"yaml: source file missing",
			slog.String("source", l.source),
		)
		return nil, nil
	default:
		// ErrorPolicyRaise (and any unrecognized value) returns a typed error.
		return nil, confii.NewLoadError(l.source, err)
	}
}
