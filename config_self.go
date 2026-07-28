// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/confiify/confii-go/selfconfig"
)

// applySelfConfig reads the self-configuration file and applies its values.
//
// G06 (Wave 22): the directory passed to [selfconfig.Read] is now the
// option-supplied WorkingDir (defaulting to "." when unset, matching
// pre-G06 behavior) instead of an unconditional literal ".". This lets
// callers using [WithWorkingDir] get self-config discovery rooted at
// their chosen base instead of the process CWD.
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
	if !opts.isSet("deep_merge") && settings.DeepMerge != nil {
		opts.DeepMerge = *settings.DeepMerge
	}
	if !opts.isSet("merge_strategy") && settings.MergeStrategy != "" {
		strategy, err := parseSelfConfigMergeStrategy(settings.MergeStrategy)
		if err != nil {
			return err
		}
		opts.MergeStrategy = &strategy
	}
	if !opts.isSet("merge_strategy_map") && len(settings.MergeStrategyMap) > 0 {
		strategyMap := make(map[string]MergeStrategy, len(settings.MergeStrategyMap))
		for path, value := range settings.MergeStrategyMap {
			strategy, err := parseSelfConfigMergeStrategy(value)
			if err != nil {
				return &ConfigError{
					Op: "ApplySelfConfig",
					Err: fmt.Errorf(
						"%w: invalid merge_strategy_map value for path %q: %w",
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
	if !opts.isSet("on_error") && settings.OnError != "" {
		// G07: validate the on_error string instead of blindly coercing.
		// Previously any string (including misspellings like "warning"
		// or "fatal") was accepted as an ErrorPolicy and downstream
		// switches treated unknown values as Warn-or-Ignore, which made
		// misconfiguration silent.
		policy, err := ParseErrorPolicy(settings.OnError)
		if err != nil {
			return err
		}
		opts.OnError = policy
	}
	if !opts.isSet("loaders") && len(settings.DefaultFiles) > 0 {
		// D07 / G19-residual: propagate the resolved Config-level
		// OnError policy to each fileAutoLoader so that missing /
		// malformed self-config-discovered files are dispatched with
		// the same semantics as explicit-loader paths (e.g.
		// loader.NewYAML). Pre-D07 the auto-loader unconditionally
		// swallowed missing files and decoded YAML directly into
		// map[string]any, bypassing both the policy contract and the
		// Wave 9 D01 normalization.
		for _, f := range settings.DefaultFiles {
			opts.Loaders = append(opts.Loaders, &fileAutoLoader{
				path:        f,
				errorPolicy: opts.OnError,
				logger:      opts.Logger,
			})
		}
	}

	// G04: wire the four parsed-but-unused settings into options.
	//
	// `default_prefix` is the documented default for the env-prefix
	// loader pipeline (Wave 12 G02 / Wave 13 G03). It only takes effect
	// when neither an explicit [WithEnvPrefix] nor a self-config
	// `env_prefix` (which already wins above) has populated EnvPrefix —
	// otherwise it would silently override more-specific configuration.
	if !opts.isSet("env_prefix") && opts.EnvPrefix == "" && settings.DefaultPrefix != "" {
		// This is a compatibility alias, not an explicit constructor
		// option. New installs the corresponding environment loader only
		// after all declarative sources have been materialized, ensuring
		// environment variables retain highest precedence.
		opts.EnvPrefix = settings.DefaultPrefix
	}

	// `log_level` constructs a *slog.Logger and assigns it to opts.Logger.
	// Mirrors the G07 invalid-on_error pattern: an unrecognised level is
	// surfaced as a typed *ConfigError instead of silently coerced.
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

	// `secrets` installs a built-in secret-resolution hook from a
	// declarative {provider: env|...} map. Explicit [WithSecretHook]
	// wins. Unrecognised providers surface as typed *ConfigError.
	if !opts.isSet("secret_hook") && opts.SecretHook == nil && len(settings.Secrets) > 0 {
		h, err := buildSelfConfigSecretHook(settings.Secrets)
		if err != nil {
			return err
		}
		opts.SecretHook = h
		provider, _ := settings.Secrets["provider"].(string)
		opts.selfConfigSecretProvider = strings.ToLower(strings.TrimSpace(provider))
	}
	return nil
}

// parseSelfConfigLogLevel translates a self-config log_level string into
// the [slog.Level] enum. Recognised values (case-insensitive,
// surrounding whitespace trimmed) are "debug", "info", "warn", and
// "error". Any other value — including the empty string — returns a
// typed *ConfigError wrapping [ErrConfigLoad] so misconfiguration is
// loud rather than silently coerced. Pattern mirrors [ParseErrorPolicy]
// (G07) for the on_error self-config setting.
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

// parseSelfConfigMergeStrategy translates the human-readable names accepted
// by .confii.yaml into the public MergeStrategy constants. "merge" is the
// canonical spelling; deep_merge and deep-merge are accepted aliases because
// the standard boolean option uses the deep_merge name.
func parseSelfConfigMergeStrategy(s string) (MergeStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "replace":
		return StrategyReplace, nil
	case "merge", "deep_merge", "deep-merge":
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
			Op: "ApplySelfConfig",
			Err: fmt.Errorf(
				"%w: invalid merge strategy %q (valid values: %q, %q, %q, %q, %q, %q)",
				ErrConfigLoad, s, "replace", "merge", "append", "prepend", "intersection", "union",
			),
		}
	}
}

// appendSelfConfigSource translates one self-config `sources:` entry
// into a [Loader] and appends it to opts.Loaders. Recognised types
// match the file-format dispatch already implemented by
// [fileAutoLoader] (yaml, yml, json, toml, ini, cfg, env, envfile)
// plus "environment" / "env-vars" for an OS-environment loader keyed
// by `prefix`. Unknown types surface as typed *ConfigError so operator
// typos in `.confii.yaml` fail loudly instead of silently dropping a
// declared source.
func appendSelfConfigSource(ctx context.Context, opts *options, src map[string]any) error {
	rawType, _ := src["type"].(string)
	t := strings.ToLower(strings.TrimSpace(rawType))
	switch t {
	case "environment_files", "environment-files":
		loaders, err := buildEnvironmentFileLoaders(opts, src)
		if err != nil {
			return err
		}
		opts.Loaders = append(opts.Loaders, loaders...)
		return nil
	case "yaml", "yml", "json", "toml", "ini", "cfg", "envfile":
		path, _ := src["path"].(string)
		if path == "" {
			return &ConfigError{
				Op:  "ApplySelfConfig",
				Err: fmt.Errorf("%w: self-config source of type %q is missing a `path` field", ErrConfigLoad, t),
			}
		}
		opts.Loaders = append(opts.Loaders, &fileAutoLoader{
			path:        path,
			errorPolicy: opts.OnError,
			logger:      opts.Logger,
		})
		return nil
	case "env":
		// Two shapes share the "env" type label: a `path:` field
		// designates a .env file (handled by fileAutoLoader's envfile
		// branch); a `prefix:` field designates an OS-environment
		// variable loader (the G03 envPrefixAutoLoader). Disambiguate
		// by looking at the keys present.
		if path, _ := src["path"].(string); path != "" {
			opts.Loaders = append(opts.Loaders, &fileAutoLoader{
				path:        path,
				errorPolicy: opts.OnError,
				logger:      opts.Logger,
			})
			return nil
		}
		prefix, _ := src["prefix"].(string)
		if prefix == "" {
			return &ConfigError{
				Op:  "ApplySelfConfig",
				Err: fmt.Errorf("%w: self-config source of type %q requires either `path` (for .env files) or `prefix` (for OS env vars)", ErrConfigLoad, t),
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
	case "environment", "env-vars":
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
