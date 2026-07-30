// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/internal/formatparse"
	"github.com/confiify/confii-go/v2/internal/typecoerce"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
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

	if _, err := os.Stat(l.path); err != nil {
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
	case formatparse.FormatYAML:
		return l.loadYAML()
	case formatparse.FormatJSON:
		return l.loadJSON()
	case formatparse.FormatTOML:
		return l.loadTOML()
	case formatparse.FormatINI:
		return l.loadINI()
	case formatparse.FormatEnvFile:
		return l.loadEnvFile()
	default:
		// Unknown extensions are typed format errors, not a
		// silent YAML fallback. Operator typos (config.xml, config.cong)
		// surface visibly instead of producing a misleading YAML parse
		// error or — worse — silently parsing arbitrary content as YAML.
		return nil, NewFormatError(l.path, string(format),
			fmt.Errorf("unsupported declarative file source %q (supported:.yaml,.yml,.json,.toml,.ini,.cfg,.env)", l.path))
	}
}

func (l *fileAutoLoader) loadYAML() (map[string]any, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, NewLoadError(l.path, err)
	}
	if err := formatparse.ValidateDeclaredContent(formatparse.FormatYAML, data); err != nil {
		return nil, NewFormatError(l.path, "yaml", err)
	}
	// Decode into an untyped value so we can normalize maps with
	// non-string keys via dictutil.NormalizeKeys (the same helper that
	// loader.YAMLLoader.Load uses). gopkg.in/yaml.v3 can emit
	// map[interface{}]interface{} for any map containing a non-string
	// key — leaking that incompatible shape into the rest of the
	// library.
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, NewFormatError(l.path, "yaml", err)
	}
	if raw == nil {
		return nil, nil
	}
	normalizedAny, nerr := dictutil.NormalizeKeys(raw)
	if nerr != nil {
		// Typed key-collision or key-coercion errors propagate as
		// format errors so the operator sees the exact ambiguity.
		return nil, NewFormatError(l.path, "yaml", nerr)
	}
	normalized, ok := normalizedAny.(map[string]any)
	if !ok {
		// Top-level scalar / sequence YAML documents cannot be
		// represented as a map[string]any; surface as a typed format
		// error rather than silently dropping the data.
		return nil, NewFormatError(l.path, "yaml",
			fmt.Errorf("expected top-level mapping, got %T", raw))
	}
	return normalized, nil
}

func (l *fileAutoLoader) loadJSON() (map[string]any, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, NewLoadError(l.path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, NewFormatError(l.path, "json", err)
	}
	return result, nil
}

func (l *fileAutoLoader) loadTOML() (map[string]any, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, NewLoadError(l.path, err)
	}
	if err := formatparse.ValidateDeclaredContent(formatparse.FormatTOML, data); err != nil {
		return nil, NewFormatError(l.path, "toml", err)
	}
	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		return nil, NewFormatError(l.path, "toml", err)
	}
	return result, nil
}

func (l *fileAutoLoader) loadINI() (map[string]any, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, NewLoadError(l.path, err)
	}
	if err := formatparse.ValidateDeclaredContent(formatparse.FormatINI, data); err != nil {
		return nil, NewFormatError(l.path, "ini", err)
	}
	cfg, err := ini.Load(data)
	if err != nil {
		return nil, NewFormatError(l.path, "ini", err)
	}
	result := make(map[string]any)
	for _, section := range cfg.Sections() {
		name := section.Name()
		// Mirror loader.INILoader: the synthetic
		// DEFAULT section holds key/value pairs preceding the first
		// explicit [section] header. Surface those as root-level keys
		// so defaults-only INI files round-trip into the configuration
		// (a bare `host = localhost` without a section).
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

func (l *fileAutoLoader) loadEnvFile() (map[string]any, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, NewLoadError(l.path, err)
	}
	if err := formatparse.ValidateDeclaredContent(formatparse.FormatEnvFile, data); err != nil {
		return nil, NewFormatError(l.path, "dotenv", err)
	}

	result := make(map[string]any)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			// Mirror loader.EnvFileLoader's malformed-line behavior:
			// Raise returns a typed *ConfigError, Warn logs, Ignore
			// silently skips.
			switch l.errorPolicy {
			case ErrorPolicyIgnore:
				continue
			case ErrorPolicyWarn:
				logger := l.logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.Warn(
					"envfile: malformed line skipped (missing '=')",
					slog.String("source", l.path),
					slog.Int("line", lineNum),
					slog.String("content", line),
				)
				continue
			default:
				return nil, NewLoadError(
					l.path,
					fmt.Errorf("malformed line %d: missing '=' separator: %q", lineNum, line),
				)
			}
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = unquoteEnvFileValue(value)
		parsed := typecoerce.ParseScalar(value, false)
		if strings.Contains(key, ".") {
			_ = dictutil.SetNested(result, key, parsed)
		} else {
			result[key] = parsed
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, NewLoadError(l.path, err)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// unquoteEnvFileValue mirrors the loader.EnvFileLoader unquoting rules
// (single-quoted literal, double-quoted with \n / \t escapes, otherwise
// strip trailing inline comments after " #"). Kept in sync with
// loader/envfile.go's unquoteEnvValue.
func unquoteEnvFileValue(value string) string {
	if len(value) >= 2 {
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
		if value[0] == '"' && value[len(value)-1] == '"' {
			inner := value[1 : len(value)-1]
			inner = strings.ReplaceAll(inner, `\n`, "\n")
			inner = strings.ReplaceAll(inner, `\t`, "\t")
			return inner
		}
	}
	if idx := strings.Index(value, " #"); idx != -1 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
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
