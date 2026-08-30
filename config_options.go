// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/confiify/confii-go/v2/hook"
)

// ErrorPolicy defines how errors raised by loaders, composers, and other
// configuration steps are surfaced to the caller. The three policies are
// strictly distinct:
//
//   - [ErrorPolicyRaise] (default): the error is wrapped in a typed
//     [*ConfigError] and returned from [New] / [Config.Reload]. The Config
//     instance is not constructed (or, on reload, the previous state is
//     preserved via rollback).
//   - [ErrorPolicyWarn]: the error is logged via the configured [*slog.Logger]
//     at warn level and loading continues with the remaining sources. Useful
//     for genuinely optional sources whose absence should be visible.
//   - [ErrorPolicyIgnore]: the error is silently swallowed; nothing is logged.
//     Use only for best-effort sources where invisible failures are acceptable.
type ErrorPolicy string

const (
	// ErrorPolicyRaise causes loader, composer, and self-config errors to
	// be returned to the caller as a typed [*ConfigError]. This is the
	// default and the safest setting.
	ErrorPolicyRaise ErrorPolicy = "raise"

	// ErrorPolicyWarn causes loader, composer, and self-config errors to
	// be logged at warn level (via the configured [*slog.Logger]) and the
	// failed source to be skipped.
	ErrorPolicyWarn ErrorPolicy = "warn"

	// ErrorPolicyIgnore causes loader, composer, and self-config errors to
	// be silently dropped. No log record is emitted.
	ErrorPolicyIgnore ErrorPolicy = "ignore"
)

// ParseErrorPolicy parses an [ErrorPolicy] from its string representation.
// Recognized values are "raise", "warn", and "ignore" (case-insensitive,
// surrounding whitespace is trimmed). Any other input — including the
// empty string — returns a typed [*ConfigError] wrapping [ErrConfigLoad]
// so callers (especially self-config readers) can surface invalid policy
// strings loudly instead of silently coercing them to a default.
func ParseErrorPolicy(s string) (ErrorPolicy, error) {
	trimmed := strings.TrimSpace(s)
	switch strings.ToLower(trimmed) {
	case string(ErrorPolicyRaise):
		return ErrorPolicyRaise, nil
	case string(ErrorPolicyWarn):
		return ErrorPolicyWarn, nil
	case string(ErrorPolicyIgnore):
		return ErrorPolicyIgnore, nil
	default:
		return "", &ConfigError{
			Op:   "ParseErrorPolicy",
			Code: ConfigErrorCodeLoad,
			Err: fmt.Errorf(
				"invalid on_error policy %q (valid values: %q, %q, %q)",
				s,
				string(ErrorPolicyRaise),
				string(ErrorPolicyWarn),
				string(ErrorPolicyIgnore),
			),
		}
	}
}

// options holds all resolved configuration options.
// Fields use pointers for booleans/strings where we need to distinguish
// "not set" from "set to zero value" (for 3-tier priority resolution).
type options struct {
	// WorkingDir is the base directory used for self-config discovery
	// (selfconfig.Read) and as the basePath for the include/defaults
	// composer (compose.New). When empty, the process CWD (".") is used.
	WorkingDir            string
	Env                   string
	EnvSwitcher           string
	Loaders               []Loader
	DynamicReloading      bool
	ReloadDebounce        time.Duration
	UseEnvExpander        bool
	UseFileResolver       bool
	UseStructuredResolver bool
	UseURLResolver        bool
	UseCommandResolver    bool
	UseTypeCasting        bool
	MergeStrategy         MergeStrategy
	MergeStrategyMap      map[string]MergeStrategy
	EnvPrefix             string
	SysenvFallback        bool
	SecretResolver        ManagedSecretResolver
	Exporters             []Exporter
	Validators            []Validator
	Schema                any
	SchemaPath            string
	ValidateOnLoad        bool
	StrictValidation      bool
	// RejectUnknownKeys fails the typed decode when the configuration
	// carries keys that the typed model does not declare.
	RejectUnknownKeys                   bool
	FreezeOnLoad                        bool
	OnError                             ErrorPolicy
	DebugMode                           bool
	Logger                              *slog.Logger
	EnvironmentStrategy                 EnvironmentStrategy
	EnvironmentConflictPolicy           EnvironmentConflictPolicy
	environmentConflictPolicyConfigured bool
	// StartupTimeout is the fallback ceiling for the complete initialization
	// pipeline when the caller's context has no deadline. A zero value disables
	// the Confii-added deadline; an explicit caller deadline always wins.
	StartupTimeout time.Duration
	// SecretResolutionConcurrency bounds eager top-level secret materialization.
	SecretResolutionConcurrency int
	// OperationTimeout bounds context-free runtime convenience methods and
	// watcher-driven reloads. Explicit *WithContext method deadlines always win.
	OperationTimeout time.Duration
	// SensitivePaths marks application-defined paths whose values must be
	// redacted even when they do not originate from a Confii secret reference.
	SensitivePaths []string

	// selfConfigSources holds declarative `.confii.*` sources until the
	// active environment has been resolved. Most source types do not depend
	// on the environment, but `environment_files` does; deferring the entire
	// ordered list preserves source precedence when the two kinds are mixed.
	selfConfigSources []map[string]any
	// selfConfigSecrets is deferred until the active environment has been
	// selected. Named secret-provider configurations may choose a different
	// default provider for each environment.
	selfConfigSecrets map[string]any
	// selfConfigSecretProvider records only the normalized provider name for
	// value-safe runtime introspection. Credentials and provider options stay
	// inside the hook/store and are never exposed through Config.
	selfConfigSecretProvider string
	// selfConfigSecretProviders records every configured provider alias for
	// value-safe introspection. It never contains provider credentials.
	selfConfigSecretProviders []string
	// SecretHook resolves secret placeholders while a configuration snapshot is
	// materialized. An explicit hook takes precedence over self-configuration.
	SecretHook hook.Func
	// hookSetups describe construction-time transformation hooks before the
	// first configuration snapshot is materialized.
	hookSetups []hookSetup
	// valueResolvers contains caller-provided ${scheme:...} resolvers.
	valueResolvers map[string]hook.ResolverFunc

	// Tracks which fields were explicitly set by user options.
	// Used to implement priority: explicit > self-config > built-in default.
	explicitlySet map[string]bool
}

// ManagedSecretResolver supplies the hook used for eager secret resolution and
// the cache invalidation operation used by [Config.RefreshSecrets]. Hook must
// be safe for concurrent calls up to the limit configured by
// [WithSecretResolutionConcurrency]. ClearCache must make the next resolution
// consult the backing store rather than a resolver-owned cache.
type ManagedSecretResolver interface {
	// Hook returns the transformation that resolves secret placeholders.
	Hook() hook.Func
	// ClearCache invalidates all resolver-owned cached values.
	ClearCache()
}

type hookSetupKind uint8

const (
	hookSetupKey hookSetupKind = iota
	hookSetupValue
	hookSetupCondition
	hookSetupGlobal
)

type hookSetup struct {
	kind      hookSetupKind
	key       string
	value     any
	condition hook.Condition
	hook      hook.Func
}

func defaultOptions() options {
	return options{
		Env:                         "",
		UseEnvExpander:              true,
		UseTypeCasting:              true,
		MergeStrategy:               StrategyMerge,
		StrictValidation:            true,
		OnError:                     ErrorPolicyRaise,
		Logger:                      slog.Default(),
		EnvironmentStrategy:         EnvironmentStrategyAuto,
		EnvironmentConflictPolicy:   EnvironmentConflictLastWins,
		StartupTimeout:              60 * time.Second,
		SecretResolutionConcurrency: 4,
		OperationTimeout:            30 * time.Second,
		ReloadDebounce:              150 * time.Millisecond,
		explicitlySet:               make(map[string]bool),
	}
}

// WithOperationTimeout sets the fallback deadline used by context-free runtime
// methods and watcher-triggered reloads. Methods whose names end in WithContext
// preserve the caller's deadline instead. Zero disables the Confii-added
// deadline; a negative duration is rejected by [NewWithContext].
func WithOperationTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.OperationTimeout = timeout
		o.explicitlySet["operation_timeout"] = true
	}
}

// WithSecretResolutionConcurrency bounds parallel provider reads while a
// snapshot is materialized. The default is four. Values below one are rejected
// by [NewWithContext] before any source is loaded.
func WithSecretResolutionConcurrency(limit int) Option {
	return func(o *options) {
		o.SecretResolutionConcurrency = limit
		o.explicitlySet["secret_resolution_concurrency"] = true
	}
}

// WithStartupTimeout sets the fallback deadline for configuration
// initialization when the context supplied to [NewWithContext] has no deadline.
// A zero duration disables the Confii-added deadline. A negative duration is
// rejected by the constructor.
//
// An existing caller deadline is never replaced or extended by this option.
func WithStartupTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.StartupTimeout = timeout
		o.explicitlySet["startup_timeout"] = true
	}
}

// WithEnvironmentStrategy selects the project's environment model. Auto uses
// sectioned documents unless a declared environment_files source makes the
// named-files model explicit. Hybrid must be selected deliberately when both
// models are present.
func WithEnvironmentStrategy(strategy EnvironmentStrategy) Option {
	return func(o *options) {
		o.EnvironmentStrategy = strategy
		o.explicitlySet["environment_strategy"] = true
	}
}

// WithEnvironmentConflictPolicy selects how duplicate keys are handled when
// [EnvironmentStrategyHybrid] combines sectioned and named-file sources. The
// option has no effect for the other environment strategies.
func WithEnvironmentConflictPolicy(policy EnvironmentConflictPolicy) Option {
	return func(o *options) {
		o.EnvironmentConflictPolicy = policy
		o.environmentConflictPolicyConfigured = true
		o.explicitlySet["environment_conflict_policy"] = true
	}
}

// isSet returns true if the given option was explicitly set by the user.
func (o *options) isSet(key string) bool {
	return o.explicitlySet[key]
}

// Option configures a Config during construction. Constructor options take
// precedence over equivalent values discovered in project self-configuration.
type Option func(*options)

// WithWorkingDir sets the project directory used for self-configuration
// discovery and relative composition paths. Relative loader paths retain their
// loader-defined semantics. An empty value uses the process working directory.
func WithWorkingDir(dir string) Option {
	return func(o *options) {
		o.WorkingDir = dir
		o.explicitlySet["working_dir"] = true
	}
}

// WithEnv sets the active environment name, such as "production". An
// explicitly supplied environment takes priority over [WithEnvSwitcher] and
// the self-configured default regardless of option order. Passing an empty
// string is an explicit selection and therefore also disables switcher lookup.
func WithEnv(env string) Option {
	return func(o *options) {
		o.Env = env
		o.explicitlySet["env"] = true
		// Lock out the env-switcher OS-variable pathway: an explicit
		// WithEnv must win, even if WithEnvSwitcher is supplied later
		// in the option list or seeded from .confii.yaml.
		o.EnvSwitcher = ""
		o.explicitlySet["env_switcher"] = true
	}
}

// WithEnvSwitcher sets the OS environment variable name whose value selects the active environment.
//
// This option is a no-op when [WithEnv] has already been applied,
// because explicit code-level environment selection always outranks an
// OS-variable lookup. The order in which options appear in the [New]
// argument list does not matter — [WithEnv] wins regardless of order.
func WithEnvSwitcher(envVar string) Option {
	return func(o *options) {
		// Explicit WithEnv always wins. Skip recording the
		// env-switcher hint so the resolver does not later overwrite
		// the explicitly chosen env with an OS-variable value.
		if o.explicitlySet["env"] {
			return
		}
		o.EnvSwitcher = envVar
		o.explicitlySet["env_switcher"] = true
	}
}

// WithLoaders sets the complete ordered source list. Later loaders have higher
// precedence under the configured merge strategy. Supplying this option makes
// the list authoritative over declarative self-config sources; an empty list
// intentionally disables those sources.
func WithLoaders(loaders ...Loader) Option {
	return func(o *options) { o.Loaders = loaders; o.explicitlySet["loaders"] = true }
}

// WithDynamicReloading enables watcher-driven reloads for trackable local file
// sources after successful construction. Remote and process-environment
// sources are refreshed only when another reload trigger runs. Call
// [Config.Close] or [Config.StopWatching] to release the watcher.
//
// This option is mutually exclusive with [WithFreezeOnLoad](true)
// — combining them is rejected at [confii.New] time with a typed
// [*ConfigError] wrapping [ErrConfigLoad], because a frozen Config
// refuses every reload and the watcher would emit only failure logs.
// Pick one: either keep the Config mutable with dynamic reloading, or
// freeze it for safety and rely on process restarts for changes.
func WithDynamicReloading(v bool) Option {
	return func(o *options) { o.DynamicReloading = v; o.explicitlySet["dynamic_reloading"] = true }
}

// WithReloadDebounce sets the trailing-edge interval used to coalesce bursts
// of filesystem events before a watcher-driven reload. The default is 150ms.
// Zero reloads immediately; negative values are rejected by [NewWithContext].
// Explicit [Config.Reload] calls are never delayed.
func WithReloadDebounce(interval time.Duration) Option {
	return func(o *options) {
		o.ReloadDebounce = interval
		o.explicitlySet["reload_debounce"] = true
	}
}

// WithSensitivePaths marks dot-separated configuration paths as sensitive in
// every published snapshot. Explicit paths are combined with paths discovered
// from secret references and are redacted by diffs, version comparisons,
// source inspection, generated documentation, and schema examples. Marking a
// parent path also protects all descendants. Paths are validated and copied
// during construction.
func WithSensitivePaths(paths ...string) Option {
	return func(o *options) {
		o.SensitivePaths = paths
		o.explicitlySet["sensitive_paths"] = true
	}
}

// WithEnvExpander controls ${VAR} and ${env:VAR} expansion during snapshot
// materialization. Unknown variables remain unchanged. The default is true.
func WithEnvExpander(v bool) Option {
	return func(o *options) { o.UseEnvExpander = v; o.explicitlySet["use_env_expander"] = true }
}

// WithFileResolver controls ${file:path} expansion during snapshot
// materialization. Relative paths resolve from WithWorkingDir, or from the
// process working directory when no working directory is configured. The
// default is false because this feature intentionally grants configuration
// files read access to local project files.
func WithFileResolver(v bool) Option {
	return func(o *options) { o.UseFileResolver = v; o.explicitlySet["use_file_resolver"] = true }
}

// WithStructuredResolver controls ${json:path#field}, ${yaml:path#field},
// ${json:self#field}, and ${yaml:self#field} expansion during snapshot
// materialization. It works for configurations loaded from any source format
// because it operates after parsing on Confii's configuration tree. The default
// is false.
func WithStructuredResolver(v bool) Option {
	return func(o *options) { o.UseStructuredResolver = v; o.explicitlySet["use_structured_resolver"] = true }
}

// WithURLResolver controls ${url:http://...} expansion during snapshot
// materialization. The default is false because this feature performs network
// I/O selected by configuration values.
func WithURLResolver(v bool) Option {
	return func(o *options) { o.UseURLResolver = v; o.explicitlySet["use_url_resolver"] = true }
}

// WithCommandResolver controls ${cmd:command} expansion during snapshot
// materialization. The command runs through the platform shell and stdout
// becomes the resolved value. The default is false because this feature grants
// trusted configuration command-execution capability.
func WithCommandResolver(v bool) Option {
	return func(o *options) { o.UseCommandResolver = v; o.explicitlySet["use_command_resolver"] = true }
}

// WithValueResolver registers a custom ${scheme:...} resolver. Custom
// resolvers run in the same materialization phase as built-in value resolvers
// and override a built-in resolver when the scheme name is the same.
func WithValueResolver(scheme string, resolver hook.ResolverFunc) Option {
	return func(o *options) {
		if o.valueResolvers == nil {
			o.valueResolvers = make(map[string]hook.ResolverFunc)
		}
		o.valueResolvers[scheme] = resolver
	}
}

// WithTypeCasting controls conversion of canonical string booleans, integers,
// and floating-point numbers during materialization. The default is true;
// disabling it preserves loaded strings verbatim, including through
// [Config.Typed] and [Config.TypedCopy], whose decode then requires the
// input to already carry each field's declared type.
func WithTypeCasting(v bool) Option {
	return func(o *options) { o.UseTypeCasting = v; o.explicitlySet["use_type_casting"] = true }
}

// WithMergeStrategy sets the strategy used wherever no more-specific path
// override is present. The default is [StrategyMerge].
func WithMergeStrategy(s MergeStrategy) Option {
	return func(o *options) { o.MergeStrategy = s; o.explicitlySet["merge_strategy"] = true }
}

// WithMergeStrategyMap sets merge strategies for dot-separated paths. Exact
// matches take precedence; descendants inherit the most-specific parent path.
// Paths not covered by the map use [WithMergeStrategy]'s value, or deep merge
// when no explicit default was supplied. The map is read during construction;
// callers should not mutate it concurrently.
func WithMergeStrategyMap(m map[string]MergeStrategy) Option {
	return func(o *options) { o.MergeStrategyMap = m; o.explicitlySet["merge_strategy_map"] = true }
}

// WithEnvPrefix auto-adds an environment-variable loader for this prefix.
//
// The prefix is uppercased. A double underscore represents a nested key, so
// APP_DB__HOST maps to db.host. An equivalent explicitly configured
// environment loader retains its original position and is not duplicated.
func WithEnvPrefix(prefix string) Option {
	return func(o *options) {
		o.EnvPrefix = prefix
		o.explicitlySet["env_prefix"] = true

		if prefix == "" {
			return
		}

		// Append the environment loader to the load pipeline so
		// env vars are merged in like any other source. Match the
		// canonical loader.EnvironmentLoader source identifier so Confii
		// can dedupe against an explicitly-supplied env loader.
		upper := strings.ToUpper(prefix)
		wantSource := "environment:" + upper
		for _, existing := range o.Loaders {
			if existing != nil && existing.Source() == wantSource {
				// Avoid applying an equivalent explicit loader twice.
				return
			}
		}
		o.Loaders = append(o.Loaders, &envPrefixAutoLoader{prefix: upper})
	}
}

// ensureEnvPrefixLoader finalizes the EnvPrefix option after declarative
// self-config sources have been materialized. An auto-loader created while
// applying constructor options is moved to the end so environment variables
// override file and remote sources. A caller-supplied equivalent loader keeps
// its chosen position.
func ensureEnvPrefixLoader(o *options) {
	if o.EnvPrefix == "" {
		return
	}
	upper := strings.ToUpper(o.EnvPrefix)
	wantSource := "environment:" + upper
	autoIndex := -1
	for i, existing := range o.Loaders {
		if existing == nil || existing.Source() != wantSource {
			continue
		}
		if _, auto := existing.(*envPrefixAutoLoader); !auto {
			return
		}
		autoIndex = i
		break
	}
	if autoIndex >= 0 {
		o.Loaders = append(o.Loaders[:autoIndex], o.Loaders[autoIndex+1:]...)
	}
	o.Loaders = append(o.Loaders, &envPrefixAutoLoader{prefix: upper})
}

// WithSysenvFallback allows a missing key to be read from the process
// environment. Dot-separated keys are converted to uppercase underscore names
// (for example, database.host becomes DATABASE_HOST); [WithEnvPrefix] prepends
// its normalized prefix. The fallback applies to reads only and does not add
// the value to the published snapshot.
func WithSysenvFallback(v bool) Option {
	return func(o *options) { o.SysenvFallback = v; o.explicitlySet["sysenv_fallback"] = true }
}

// WithExporter registers an application-defined serializer by the stable
// lowercase name returned from [Exporter.Format]. A custom exporter replaces
// a built-in exporter with the same name, allowing applications to customize
// JSON, YAML, or TOML output, and may also introduce a new format.
//
// Registration is validated by [NewWithContext]. Nil exporters, typed-nil
// exporters, and empty or non-canonical format names are rejected before any
// source is loaded. Repeating the option for the same format uses the last
// registered exporter.
func WithExporter(exporter Exporter) Option {
	return func(o *options) {
		o.Exporters = append(o.Exporters, exporter)
		o.explicitlySet["exporters"] = true
	}
}

// WithValidator adds an application-defined validation rule to the snapshot
// lifecycle. Registering a validator enables validation. Custom validators run
// in registration order after JSON Schema validation and before typed-struct
// validation. A failure rejects construction or the candidate mutation
// transaction without publishing partial configuration.
//
// Confii passes each validator an independent copy of the candidate map, so a
// validator cannot mutate the snapshot that will be published. Nil and
// typed-nil validators are rejected by [NewWithContext].
func WithValidator(validator Validator) Option {
	return func(o *options) {
		o.Validators = append(o.Validators, validator)
		o.ValidateOnLoad = true
		o.explicitlySet["validators"] = true
		o.explicitlySet["validate_on_load"] = true
	}
}

// WithSchema sets an inline JSON Schema represented as map[string]any. Typed
// struct validation is derived from Config's type parameter T and its
// `validate` tags rather than from this option. Validation runs when
// [WithValidateOnLoad](true) is also enabled.
func WithSchema(schema any) Option {
	return func(o *options) { o.Schema = schema; o.explicitlySet["schema"] = true }
}

// WithSchemaPath sets the path to a JSON Schema document. The path is compiled
// during construction and failures wrap [ErrConfigValidation]. An inline
// schema supplied through [WithSchema] takes precedence.
func WithSchemaPath(path string) Option {
	return func(o *options) { o.SchemaPath = path; o.explicitlySet["schema_path"] = true }
}

// WithValidateOnLoad controls validation before each candidate snapshot is
// published, including construction, reload, extension, override, and secret
// refresh. Config[any] requires a JSON Schema; a struct T is decoded and
// validated from its `validate` tags.
func WithValidateOnLoad(v bool) Option {
	return func(o *options) { o.ValidateOnLoad = v; o.explicitlySet["validate_on_load"] = true }
}

// WithRejectUnknownKeys controls whether configuration keys that the typed
// model does not declare are an error. The default is false, which leaves an
// undeclared key silently unused, so a mistyped key such as "prot" for "port"
// produces a zero-valued field indistinguishable from an absent setting.
// Enabling it fails the typed decode instead, naming the offending keys.
// Config[any] is unaffected: without declared fields there is nothing for a
// key to be unused against.
func WithRejectUnknownKeys(v bool) Option {
	return func(o *options) {
		o.RejectUnknownKeys = v
		o.explicitlySet["reject_unknown_keys"] = true
	}
}

// WithStrictValidation controls typed-struct validation failures. When true
// (the default), a violation rejects the candidate with
// [ErrConfigValidation]. When false, typed violations are logged and the
// candidate may be published. JSON Schema violations remain fatal.
// It governs how a failure is reported, not which conditions fail; use
// [WithRejectUnknownKeys] to make undeclared keys a failure.
func WithStrictValidation(v bool) Option {
	return func(o *options) { o.StrictValidation = v; o.explicitlySet["strict_validation"] = true }
}

// WithFreezeOnLoad freezes the config immediately after the initial load
// succeeds. A frozen Config rejects every mutation API ([Config.Set],
// [Config.Reload], [Config.Extend], [Config.RollbackToVersion]) with
// [ErrConfigFrozen].
//
// This option is mutually exclusive with [WithDynamicReloading](true)
// — combining them is rejected at [confii.New] time with a typed
// [*ConfigError]. A frozen Config under a watcher would emit only
// "config is frozen" reload errors. Use one or the other.
func WithFreezeOnLoad(v bool) Option {
	return func(o *options) { o.FreezeOnLoad = v; o.explicitlySet["freeze_on_load"] = true }
}

// WithOnError sets the error-handling policy applied when a loader's
// Load call fails or when composition (`_include` / `_defaults`)
// processing returns an error. The three behaviors are strictly distinct:
//
//   - [ErrorPolicyRaise] (default): the error is returned from [New] or
//     [Config.Reload]; on Reload the previous state is preserved via
//     rollback.
//   - [ErrorPolicyWarn]: the error is logged at warn level via the
//     Config's logger and the source is skipped.
//   - [ErrorPolicyIgnore]: the error is silently swallowed. No log
//     record is emitted.
//
// Per-loader policies (for example [loader.WithEnvFileErrorPolicy]) take
// effect inside the loader before the error reaches this Config-level
// dispatch; setting both is supported and useful for routing different
// classes of failures.
func WithOnError(p ErrorPolicy) Option {
	return func(o *options) { o.OnError = p; o.explicitlySet["on_error"] = true }
}

// WithDebugMode controls retention of complete per-key override histories.
// Basic source attribution remains available when disabled. Enable it when
// [Config.GetOverrideHistory] or complete debug reports are required.
func WithDebugMode(v bool) Option {
	return func(o *options) { o.DebugMode = v; o.explicitlySet["debug_mode"] = true }
}

// WithLogger sets the logger used for warnings, lifecycle diagnostics, and
// recovered callback panics. The logger must be non-nil; omit this option to
// use [slog.Default].
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.Logger = l; o.explicitlySet["logger"] = true }
}

// WithSecretHook registers a secret-resolution hook on the
// Config's hook processor at construction time. After sources are merged and
// the active environment is selected, New invokes the hook across the final
// effective configuration and fails atomically if a required secret cannot be
// resolved. Ordinary reads then consume the ready in-memory snapshot without
// rerunning the hook. Mutations and explicit refreshes materialize a candidate
// snapshot through the same frozen plan before publication.
//
// This option is the explicit-wins counterpart to a self-config
// `secrets:` declaration. When both are supplied, the explicit hook wins
// and the self-config-derived hook is dropped (consistent with the rest
// of the explicit > self-config priority).
func WithSecretHook(h hook.Func) Option {
	return func(o *options) { o.SecretHook = h; o.explicitlySet["secret_hook"] = true }
}

// WithKeyHook registers a transformation for one exact dot-separated key. Key
// hooks run before value, condition, and global hooks.
//
//	cfg, err := confii.New[AppConfig](
//		confii.WithKeyHook("server.host", func(ctx context.Context, key string, value any) (any, error) {
//			host, ok := value.(string)
//			if !ok { return nil, fmt.Errorf("%s must be a string", key) }
//			return strings.TrimSpace(host), nil
//		}),
//	)
func WithKeyHook(key string, h hook.Func) Option {
	return func(o *options) {
		o.hookSetups = append(o.hookSetups, hookSetup{kind: hookSetupKey, key: key, hook: h})
	}
}

// WithValueHook registers a transformation selected by deep equality with the
// value entering the value-hook stage. Key-hook output therefore participates
// in matching, and maps and slices are supported.
func WithValueHook(value any, h hook.Func) Option {
	return func(o *options) {
		o.hookSetups = append(o.hookSetups, hookSetup{kind: hookSetupValue, value: value, hook: h})
	}
}

// WithConditionHook registers a predicate and transformation. The predicate
// receives the value produced by key and value hooks; false skips the hook,
// while an error rejects the candidate snapshot.
func WithConditionHook(condition hook.Condition, h hook.Func) Option {
	return func(o *options) {
		o.hookSetups = append(o.hookSetups, hookSetup{kind: hookSetupCondition, condition: condition, hook: h})
	}
}

// WithGlobalHook registers a transformation for every leaf value. Global hooks
// run after key, value, and condition hooks in registration order.
func WithGlobalHook(h hook.Func) Option {
	return func(o *options) {
		o.hookSetups = append(o.hookSetups, hookSetup{kind: hookSetupGlobal, hook: h})
	}
}

// WithSecretResolver wires a managed resolver into eager initialization and
// explicit refresh. Unlike [WithSecretHook], this option also gives Confii a
// cache invalidation handle, so [Config.RefreshSecrets] reads through to the
// backing store instead of accepting an unexpired resolver cache entry.
//
//	store := secret.NewDictStore(map[string]any{"database/password": "dev-only"})
//	resolver := secret.NewResolver(store, secret.WithCacheTTL(5*time.Minute))
//	cfg, err := confii.New[AppConfig](
//		confii.WithLoaders(loader.NewYAML("config.yaml")),
//		confii.WithSecretResolver(resolver),
//	)
//	// ${secret:database/password} is resolved before New returns.
func WithSecretResolver(resolver ManagedSecretResolver) Option {
	return func(o *options) {
		o.SecretResolver = resolver
		o.explicitlySet["secret_hook"] = true
	}
}
