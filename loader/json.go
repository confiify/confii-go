// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"

	confii "github.com/confiify/confii-go/v2"
)

// JSONLoader loads configuration from a JSON file.
//
// Absence of the configured file is governed by the loader's
// [confii.ErrorPolicy] (default [confii.ErrorPolicyRaise]):
//
//   - ErrorPolicyRaise:  Load returns a typed [*confii.ConfigError]
//     wrapping [confii.ErrConfigLoad].
//   - ErrorPolicyWarn:   the missing file is logged via the configured
//     [*slog.Logger] and Load returns (nil, nil).
//   - ErrorPolicyIgnore: the missing file is silently skipped.
type JSONLoader struct {
	source      string
	errorPolicy confii.ErrorPolicy
	logger      *slog.Logger
}

// JSONOption configures the JSONLoader.
type JSONOption func(*JSONLoader)

// WithJSONErrorPolicy sets how the loader reacts to load-time failures
// (missing file, I/O error, malformed JSON). Defaults to
// [confii.ErrorPolicyRaise].
func WithJSONErrorPolicy(p confii.ErrorPolicy) JSONOption {
	return func(l *JSONLoader) { l.errorPolicy = p }
}

// WithJSONLogger sets the logger used when the error policy is
// [confii.ErrorPolicyWarn]. Nil is ignored. Defaults to [slog.Default()].
func WithJSONLogger(logger *slog.Logger) JSONOption {
	return func(l *JSONLoader) {
		if logger != nil {
			l.logger = logger
		}
	}
}

// NewJSON creates a new JSON loader for the given file path. The loader's
// error policy defaults to [confii.ErrorPolicyRaise]; pass
// [WithJSONErrorPolicy] to change it for genuinely optional files.
func NewJSON(path string, opts ...JSONOption) *JSONLoader {
	l := &JSONLoader{
		source:      path,
		errorPolicy: confii.ErrorPolicyRaise,
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Source returns the configured path exactly as supplied to NewJSON.
func (l *JSONLoader) Source() string { return l.source }

// Load reads one JSON object from the configured path. JSON numbers follow
// encoding/json's default float64 representation. File errors wrap
// [confii.ErrConfigLoad] and decoding errors wrap [confii.ErrConfigFormat],
// subject to the configured missing-file policy. Local reads are synchronous;
// ctx is accepted for Loader compatibility but is not observed.
func (l *JSONLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return l.handleMissing(err)
		}
		return nil, confii.NewLoadError(l.source, err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, confii.NewFormatError(l.source, "json", err)
	}
	return result, nil
}

// handleMissing dispatches an os.ErrNotExist condition through the
// configured error policy.
func (l *JSONLoader) handleMissing(err error) (map[string]any, error) {
	switch l.errorPolicy {
	case confii.ErrorPolicyIgnore:
		return nil, nil
	case confii.ErrorPolicyWarn:
		l.logger.Warn(
			"json: source file missing",
			slog.String("source", l.source),
		)
		return nil, nil
	default:
		return nil, confii.NewLoadError(l.source, err)
	}
}
