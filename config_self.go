// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/confiify/confii-go/v2/internal/formatparse"
	"github.com/confiify/confii-go/v2/selfconfig"
)

// applySelfConfig reads the self-configuration file and applies its values.
//
// The directory passed to [selfconfig.Read] is the option-supplied WorkingDir,
// defaulting to "." when unset. Callers using [WithWorkingDir] therefore get
// self-config discovery rooted at their chosen base instead of the process CWD.
func applySelfConfig(opts *options) error {
	workdir := opts.WorkingDir
	if workdir == "" {
		workdir = "."
	}
	settings, err := selfconfig.Read(workdir)
	if err != nil {
		return err
	}
	if settings == nil {
		return nil
	}

	if !opts.isSet("env") && settings.DefaultEnvironment != "" {
		opts.Env = settings.DefaultEnvironment
	}
	if !opts.isSet("env_switcher") && settings.EnvSwitcher != "" {
		opts.EnvSwitcher = settings.EnvSwitcher
	}
	if !opts.isSet("env_prefix") && settings.EnvPrefix != "" {
		opts.EnvPrefix = settings.EnvPrefix
	}
	if !opts.isSet("sysenv_fallback") && settings.SysenvFallback != nil {
		opts.SysenvFallback = *settings.SysenvFallback
	}
	if !opts.isSet("merge_strategy") && settings.Merge.Default != "" {
		strategy, err := parseSelfConfigMergeStrategy(settings.Merge.Default)
		if err != nil {
			return err
		}
		opts.MergeStrategy = strategy
	}
	if !opts.isSet("merge_strategy_map") && len(settings.Merge.Paths) > 0 {
		strategyMap := make(map[string]MergeStrategy, len(settings.Merge.Paths))
		for path, value := range settings.Merge.Paths {
			strategy, err := parseSelfConfigMergeStrategy(value)
			if err != nil {
				return &ConfigError{
					Op: "ApplySelfConfig",
					Err: fmt.Errorf(
						"%w: invalid merge.paths value for path %q: %w",
						ErrConfigLoad, path, err,
					),
				}
			}
			strategyMap[path] = strategy
		}
		opts.MergeStrategyMap = strategyMap
	}
	if !opts.isSet("use_env_expander") && settings.UseEnvExpander != nil {
		opts.UseEnvExpander = *settings.UseEnvExpander
	}
	if !opts.isSet("use_type_casting") && settings.UseTypeCasting != nil {
		opts.UseTypeCasting = *settings.UseTypeCasting
	}
	if !opts.isSet("validate_on_load") && settings.ValidateOnLoad != nil {
		opts.ValidateOnLoad = *settings.ValidateOnLoad
	}
	if !opts.isSet("strict_validation") && settings.StrictValidation != nil {
		opts.StrictValidation = *settings.StrictValidation
	}
	if !opts.isSet("dynamic_reloading") && settings.DynamicReloading != nil {
		opts.DynamicReloading = *settings.DynamicReloading
	}
	if !opts.isSet("freeze_on_load") && settings.FreezeOnLoad != nil {
		opts.FreezeOnLoad = *settings.FreezeOnLoad
	}
	if !opts.isSet("debug_mode") && settings.DebugMode != nil {
		opts.DebugMode = *settings.DebugMode
	}
	if !opts.isSet("schema_path") && settings.SchemaPath != "" {
		opts.SchemaPath = settings.SchemaPath
	}
	if !opts.isSet("environment_strategy") && settings.EnvironmentStrategy != "" {
		strategy, err := parseEnvironmentStrategy(settings.EnvironmentStrategy)
		if err != nil {
			return err
		}
		opts.EnvironmentStrategy = strategy
	}
	if !opts.isSet("environment_conflict_policy") && settings.EnvironmentConflictPolicy != "" {
		policy, err := parseEnvironmentConflictPolicy(settings.EnvironmentConflictPolicy)
		if err != nil {
			return err
		}
		opts.EnvironmentConflictPolicy = policy
		opts.environmentConflictPolicyConfigured = true
	}
	if !opts.isSet("startup_timeout") && settings.Startup.Timeout != "" {
		timeout, err := time.ParseDuration(strings.TrimSpace(settings.Startup.Timeout))
		if err != nil {
			return &ConfigError{
				Op: "ApplySelfConfig",
				Err: fmt.Errorf(
					"%w: invalid startup.timeout %q: %w",
					ErrConfigLoad, settings.Startup.Timeout, err,
				),
			}
		}
		opts.StartupTimeout = timeout
	}
	if !opts.isSet("secret_resolution_concurrency") && settings.SecretResolutionConcurrency != nil {
		opts.SecretResolutionConcurrency = *settings.SecretResolutionConcurrency
	}
	if !opts.isSet("operation_timeout") && settings.Runtime.Timeout != "" {
		timeout, err := time.ParseDuration(strings.TrimSpace(settings.Runtime.Timeout))
		if err != nil {
			return &ConfigError{Op: "ApplySelfConfig", Err: fmt.Errorf("%w: invalid runtime.timeout %q: %w", ErrConfigLoad, settings.Runtime.Timeout, err)}
		}
		opts.OperationTimeout = timeout
	}
	if !opts.isSet("on_error") && settings.OnError != "" {
		// Reject unsupported policy names instead of silently changing error
		// handling behavior.
		policy, err := ParseErrorPolicy(settings.OnError)
		if err != nil {
			return err
		}
		opts.OnError = policy
	}
	// log_level constructs the logger used by configuration operations.
	if !opts.isSet("logger") && settings.LogLevel != "" {
		lvl, err := parseSelfConfigLogLevel(settings.LogLevel)
		if err != nil {
			return err
		}
		handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
		opts.Logger = slog.New(handler)
	}

	// Preserve declarative sources until New has resolved EnvSwitcher. The
	// environment_files source needs the final active environment, and
	// deferring the complete list keeps mixed source ordering intact.
	// Explicit code (WithLoaders / Builder.AddLoader) still wins.
	if !opts.isSet("loaders") && len(settings.Sources) > 0 {
		opts.selfConfigSources = append([]map[string]any(nil), settings.Sources...)
	}

	// Preserve declarative secret configuration until New has selected the
	// active environment. The named-provider form can choose an
	// environment-specific default; explicit WithSecretHook still wins.
	if !opts.isSet("secret_hook") && opts.SecretHook == nil && len(settings.Secrets) > 0 {
		opts.selfConfigSecrets = maps.Clone(settings.Secrets)
	}
	return nil
}

// parseSelfConfigLogLevel translates a self-config log_level string into
// the [slog.Level] enum. Recognised values (case-insensitive,
// surrounding whitespace trimmed) are "debug", "info", "warn", and
// "error". Any other value — including the empty string — returns a
// typed *ConfigError wrapping [ErrConfigLoad] so misconfiguration is
// loud rather than silently coerced, matching [ParseErrorPolicy] for the
// on_error self-config setting.
func parseSelfConfigLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, &ConfigError{
			Op: "ApplySelfConfig",
			Err: fmt.Errorf(
				"%w: invalid log_level %q (valid values: %q, %q, %q, %q)",
				ErrConfigLoad, s, "debug", "info", "warn", "error",
			),
		}
	}
}

// parseSelfConfigMergeStrategy translates canonical self-config strategy names.
func parseSelfConfigMergeStrategy(s string) (MergeStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "replace":
		return StrategyReplace, nil
	case "shallow_merge":
		return StrategyShallowMerge, nil
	case "deep_merge":
		return StrategyMerge, nil
	case "append":
		return StrategyAppend, nil
	case "prepend":
		return StrategyPrepend, nil
	case "intersection":
		return StrategyIntersection, nil
	case "union":
		return StrategyUnion, nil
	default:
		return 0, &ConfigError{
			Op:  "ApplySelfConfig",
			Err: fmt.Errorf("%w: invalid merge strategy %q (valid values: replace, shallow_merge, deep_merge, append, prepend, intersection, union)", ErrConfigLoad, s),
		}
	}
}

// appendSelfConfigSource translates one self-config `sources:` entry
// into a [Loader] and appends it to opts.Loaders. Recognised types
// use one canonical spelling per behavior: yaml, json, toml, ini, dotenv,
// environment, and environment_files. File extensions may retain their
// conventional aliases (for example .yml and .cfg), but the declared type
// must agree with the path. Unknown types surface as typed *ConfigError so operator
// typos in `.confii.yaml` fail loudly instead of silently dropping a
// declared source.
func appendSelfConfigSource(ctx context.Context, opts *options, src map[string]any) error {
	rawType, _ := src["type"].(string)
	t := strings.ToLower(strings.TrimSpace(rawType))
	switch t {
	case "environment_files":
		loaders, err := buildEnvironmentFileLoaders(opts, src)
		if err != nil {
			return err
		}
		opts.Loaders = append(opts.Loaders, loaders...)
		return nil
	case "yaml", "json", "toml", "ini", "dotenv":
		path, _ := src["path"].(string)
		if path == "" {
			return &ConfigError{
				Op:  "ApplySelfConfig",
				Err: fmt.Errorf("%w: self-config source of type %q is missing a `path` field", ErrConfigLoad, t),
			}
		}
		format, err := declarativeSourceFormat(t, path)
		if err != nil {
			return err
		}
		opts.Loaders = append(opts.Loaders, &fileAutoLoader{
			path:        path,
			format:      format,
			errorPolicy: opts.OnError,
			logger:      opts.Logger,
		})
		return nil
	case "environment":
		prefix, _ := src["prefix"].(string)
		if prefix == "" {
			return &ConfigError{
				Op:  "ApplySelfConfig",
				Err: fmt.Errorf("%w: self-config source of type %q requires a `prefix` field", ErrConfigLoad, t),
			}
		}
		upper := strings.ToUpper(prefix)
		wantSource := "environment:" + upper
		for _, existing := range opts.Loaders {
			if existing != nil && existing.Source() == wantSource {
				return nil
			}
		}
		opts.Loaders = append(opts.Loaders, &envPrefixAutoLoader{prefix: upper})
		return nil
	default:
		loader, err := buildRegisteredSelfConfigSource(ctx, t, src)
		if err != nil {
			return err
		}
		opts.Loaders = append(opts.Loaders, loader)
		return nil
	}
}

func declarativeSourceFormat(sourceType, path string) (formatparse.Format, error) {
	cleanPath := strings.ToLower(strings.TrimSpace(path))
	extension := strings.ToLower(filepath.Ext(cleanPath))
	base := filepath.Base(cleanPath)

	var format formatparse.Format
	compatible := false
	switch sourceType {
	case "yaml":
		format = formatparse.FormatYAML
		compatible = extension == ".yaml" || extension == ".yml"
	case "json":
		format = formatparse.FormatJSON
		compatible = extension == ".json"
	case "toml":
		format = formatparse.FormatTOML
		compatible = extension == ".toml"
	case "ini":
		format = formatparse.FormatINI
		compatible = extension == ".ini" || extension == ".cfg"
	case "dotenv":
		format = formatparse.FormatEnvFile
		compatible = extension == ".env" || base == ".env" || strings.HasPrefix(base, ".env.")
	}
	if compatible {
		return format, nil
	}
	return formatparse.FormatUnknown, &ConfigError{
		Op: "ApplySelfConfig",
		Err: fmt.Errorf(
			"%w: self-config source type %q is incompatible with path %q",
			ErrConfigFormat, sourceType, path,
		),
	}
}
