// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/configmap"
	"github.com/confiify/confii-go/v2/internal/formatparse"
	"github.com/confiify/confii-go/v2/internal/typecoerce"
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

	result := make(map[string]any)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Must contain '='.
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			switch l.errorPolicy {
			case confii.ErrorPolicyIgnore:
				continue
			case confii.ErrorPolicyWarn:
				l.logger.Warn(
					"envfile: malformed line skipped (missing '=')",
					slog.String("source", l.source),
					slog.Int("line", lineNum),
					slog.String("content", line),
				)
				continue
			default:
				// ErrorPolicyRaise (and any unrecognized value) returns a typed error.
				return nil, confii.NewLoadError(
					l.source,
					fmt.Errorf("malformed line %d: missing '=' separator: %q", lineNum, line),
				)
			}
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Handle quoting.
		value = unquoteEnvValue(value)

		// Type coerce.
		parsed := typecoerce.ParseScalar(value, false)

		if err := configmap.Set(result, key, parsed); err != nil {
			switch l.errorPolicy {
			case confii.ErrorPolicyIgnore:
				continue
			case confii.ErrorPolicyWarn:
				l.logger.Warn(
					"envfile: invalid key skipped",
					slog.String("source", l.source),
					slog.Int("line", lineNum),
					slog.String("key", key),
					slog.Any("error", err),
				)
				continue
			default:
				return nil, confii.NewLoadError(
					l.source,
					fmt.Errorf("line %d: %w", lineNum, err),
				)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, confii.NewLoadError(l.source, err)
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// unquoteEnvValue handles single-quoted, double-quoted, and unquoted values.
func unquoteEnvValue(value string) string {
	if len(value) >= 2 {
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			// Single-quoted: literal, no escapes.
			return value[1 : len(value)-1]
		}
		if value[0] == '"' && value[len(value)-1] == '"' {
			// Double-quoted: process escape sequences.
			inner := value[1 : len(value)-1]
			inner = strings.ReplaceAll(inner, `\n`, "\n")
			inner = strings.ReplaceAll(inner, `\t`, "\t")
			return inner
		}
	}

	// Unquoted: strip inline comments (" #" and everything after).
	if idx := strings.Index(value, " #"); idx != -1 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}
