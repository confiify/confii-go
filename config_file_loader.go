// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/confiify/confii-go/v2/internal/configdecode"
	"github.com/confiify/confii-go/v2/internal/dotenvparse"
	"github.com/confiify/confii-go/v2/internal/formatparse"
)

// fileAutoLoader is used by declarative local-file sources. It auto-detects the file format from the file
// extension and dispatches to the same parsing logic as the user-facing
// loaders in the loader subpackage.
//
// Supported file extensions:
//
//   - .yaml, .yml: YAML — keys are recursively normalized via
//     [dictutil.NormalizeKeys] so non-string-keyed YAML (e.g. integer or
//     boolean keys) never leaks map[interface{}]interface{} into caller-
//     visible state.
//   - .json: JSON via [encoding/json].
//   - .toml: TOML via [github.com/BurntSushi/toml].
//   - .ini, .cfg: INI via [gopkg.in/ini.v1]; defaults-only sections (the
//     synthetic DEFAULT section preceding the first explicit [section]
//     header) are promoted to root keys to match [loader.INILoader].
//   - .env: KEY=VALUE pairs with comment + quote support, mirroring the
//     [loader.EnvFileLoader] format.
//
// Any other extension produces a typed *ConfigError wrapping
// [ErrConfigFormat]. Falling back to YAML is not permitted because it
// masked operator typos like `config.xml` or `config.cong`.
//
// Missing files are dispatched through the loader's [ErrorPolicy]:
// under [ErrorPolicyRaise] (default for explicit-loader parity) a typed
// *ConfigError wrapping [ErrConfigLoad] is returned; under
// [ErrorPolicyWarn] the absence is logged and (nil, nil) is returned;
// under [ErrorPolicyIgnore] the file is silently skipped. The policy
// is inherited from the Config-level [WithOnError] when applySelfConfig
// constructs the loader, providing parity with the explicit-loader
// per-loader errorPolicy contract.
type fileAutoLoader struct {
	path        string
	format      formatparse.Format
	errorPolicy ErrorPolicy
	logger      *slog.Logger
}

// Source returns the file path identifying this loader's source.
func (l *fileAutoLoader) Source() string { return l.path }

// Load reads, parses, and normalizes the configured file. See
// [fileAutoLoader] for the supported-extension list, YAML normalization, and missing-file policy
// dispatch.
func (l *fileAutoLoader) Load(_ context.Context) (map[string]any, error) {
	logger := l.logger
	if logger == nil {
		logger = slog.Default()
	}

	data, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return l.handleMissing(err, logger)
		}
		return nil, NewLoadError(l.path, err)
	}

	format := l.format
	if format == formatparse.FormatUnknown {
		format = formatparse.FromExtension(l.path)
	}
	switch format {
	case formatparse.FormatEnvFile:
		return l.loadEnvFile(data)
	case formatparse.FormatYAML, formatparse.FormatJSON, formatparse.FormatTOML, formatparse.FormatINI:
		result, decodeErr := configdecode.Map(data, format)
		if decodeErr != nil {
			return nil, NewFormatError(l.path, string(format), decodeErr)
		}
		return result, nil
	default:
		// Unknown extensions are typed format errors, not a
		// silent YAML fallback. Operator typos (config.xml, config.cong)
		// surface visibly instead of producing a misleading YAML parse
		// error or — worse — silently parsing arbitrary content as YAML.
		return nil, NewFormatError(l.path, string(format),
			fmt.Errorf("unsupported declarative file source %q (supported:.yaml,.yml,.json,.toml,.ini,.cfg,.env)", l.path))
	}
}

func (l *fileAutoLoader) loadEnvFile(data []byte) (map[string]any, error) {
	if err := formatparse.ValidateDeclaredContent(formatparse.FormatEnvFile, data); err != nil {
		return nil, NewFormatError(l.path, "dotenv", err)
	}

	return dotenvparse.Parse(data, func(issue dotenvparse.Issue) error {
		switch l.errorPolicy {
		case ErrorPolicyIgnore:
			return nil
		case ErrorPolicyWarn:
			logger := l.logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Warn(
				"envfile: malformed line skipped",
				slog.String("source", l.path),
				slog.Int("line", issue.Line),
				slog.Any("error", issue.Err),
			)
			return nil
		default:
			if issue.Line > 0 {
				return NewLoadError(l.path, fmt.Errorf("malformed line %d: %w", issue.Line, issue.Err))
			}
			return NewLoadError(l.path, issue.Err)
		}
	})
}

// handleMissing dispatches an os.ErrNotExist condition through the
// loader's configured ErrorPolicy. Mirrors loader.YAMLLoader.handleMissing
// so the explicit and self-config-discovered paths share an identical
// missing-file contract.
func (l *fileAutoLoader) handleMissing(err error, logger *slog.Logger) (map[string]any, error) {
	switch l.errorPolicy {
	case ErrorPolicyIgnore:
		return nil, nil
	case ErrorPolicyWarn:
		logger.Warn(
			"fileAutoLoader: source file missing",
			slog.String("source", l.path),
		)
		return nil, nil
	default:
		// ErrorPolicyRaise (and any unrecognized value) returns a
		// typed *ConfigError wrapping ErrConfigLoad — parity with
		// loader.NewYAML's default policy.
		return nil, NewLoadError(l.path, err)
	}
}
