// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/dotenvparse"
	"github.com/confiify/confii-go/v2/internal/formatparse"
)

// EnvFileLoader loads configuration from a .env file.
// Format: KEY=VALUE per line with support for comments, quoting, and nested keys.
//
// Lines that are blank or begin with `#` are ignored. Any other line that does
// not contain `=` is malformed; how the loader reacts is governed by the
// configured [confii.ErrorPolicy] (default [confii.ErrorPolicyRaise]):
//
//   - ErrorPolicyRaise:  Load returns a [*confii.ConfigError] identifying the
//     file path and 1-based line number of the first malformed line.
//   - ErrorPolicyWarn:   the malformed line is logged as a warning and skipped.
//   - ErrorPolicyIgnore: the malformed line is silently skipped.
type EnvFileLoader struct {
	source      string
	errorPolicy confii.ErrorPolicy
	logger      *slog.Logger
}

// EnvFileOption configures the EnvFileLoader.
type EnvFileOption func(*EnvFileLoader)

// WithEnvFileErrorPolicy sets how the loader reacts to malformed lines
// (lines that are not blank, not comments, and contain no `=`). The default
// is [confii.ErrorPolicyRaise], matching the rest of the library.
func WithEnvFileErrorPolicy(p confii.ErrorPolicy) EnvFileOption {
	return func(l *EnvFileLoader) { l.errorPolicy = p }
}

// WithEnvFileLogger sets the logger used when the error policy is
// [confii.ErrorPolicyWarn]. Defaults to [slog.Default()].
func WithEnvFileLogger(logger *slog.Logger) EnvFileOption {
	return func(l *EnvFileLoader) {
		if logger != nil {
			l.logger = logger
		}
	}
}

// NewEnvFile creates a dotenv loader. An empty path selects ".env". A missing
// file is treated as an optional source and returns nil, nil; other I/O and
// format failures return an error. The malformed-line policy defaults to
// [confii.ErrorPolicyRaise].
func NewEnvFile(path string, opts ...EnvFileOption) *EnvFileLoader {
	if path == "" {
		path = ".env"
	}
	l := &EnvFileLoader{
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
func (l *EnvFileLoader) Source() string { return l.source }

// Load parses KEY=VALUE records from the configured file. Single and double
// quotes are removed, supported escape sequences in double-quoted values are
// decoded, dot-separated keys form nested maps, and scalar values are
// converted to bool, int, or float when unambiguous. Missing files and empty
// files return nil, nil. The context is accepted for Loader compatibility;
// local reads are synchronous and do not observe cancellation.
//
// Malformed lines (non-blank, non-comment lines that do not contain `=`) are
// surfaced according to the loader's configured error policy. See
// [EnvFileLoader] for details.
func (l *EnvFileLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, confii.NewLoadError(l.source, err)
	}
	if err := formatparse.ValidateDeclaredContent(formatparse.FormatEnvFile, data); err != nil {
		return nil, confii.NewFormatError(l.source, "dotenv", err)
	}

	return dotenvparse.Parse(data, func(issue dotenvparse.Issue) error {
		switch l.errorPolicy {
		case confii.ErrorPolicyIgnore:
			return nil
		case confii.ErrorPolicyWarn:
			l.logger.Warn(
				"envfile: malformed line skipped",
				slog.String("source", l.source),
				slog.Int("line", issue.Line),
				slog.Any("error", issue.Err),
			)
			return nil
		default:
			if issue.Line > 0 {
				return confii.NewLoadError(l.source, fmt.Errorf("malformed line %d: %w", issue.Line, issue.Err))
			}
			return confii.NewLoadError(l.source, issue.Err)
		}
	})
}
