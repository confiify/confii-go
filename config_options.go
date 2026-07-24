package confii

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/confiify/confii-go/hook"
)

// ErrorPolicy defines how errors raised by loaders, composers, and other
// configuration steps are surfaced to the caller. The three policies are
// strictly distinct (G07):
//
//   - [ErrorPolicyRaise] (default): the error is wrapped in a typed
//     [*ConfigError] and returned from [New] / [Config.Reload]. The Config
//     instance is not constructed (or, on reload, the previous state is
//     preserved via rollback).
//   - [ErrorPolicyWarn]: the error is logged via the configured [*slog.Logger]
//     at warn level and loading continues with the remaining sources. Useful
//     for genuinely optional sources whose absence should be visible.
//   - [ErrorPolicyIgnore]: the error is silently swallowed; nothing is logged.
//     Use only when the source is truly best-effort and you accept that
//     failures will be invisible.
//
// Prior to G07, [ErrorPolicyIgnore] still produced a warn-level log record,
// making it indistinguishable from [ErrorPolicyWarn]. The two now behave
// differently as documented.
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
	// be silently dropped. No log record is emitted. Distinct from
	// [ErrorPolicyWarn] as of G07.
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
			Op: "ParseErrorPolicy",
			Err: fmt.Errorf(
				"%w: invalid on_error policy %q (valid values: %q, %q, %q)",
				ErrConfigLoad, s,
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
	// composer (compose.New). When empty, the process CWD (".") is used,
	// matching pre-G06 behavior. Set via [WithWorkingDir].
	WorkingDir       string
	Env              string
	EnvSwitcher      string
	Loaders          []Loader
	DynamicReloading bool
	UseEnvExpander   bool
	UseTypeCasting   bool
	DeepMerge        bool
	MergeStrategy    *MergeStrategy
	MergeStrategyMap map[string]MergeStrategy
	EnvPrefix        string
	SysenvFallback   bool
	SecretResolver   any // *secret.Resolver, kept as any to avoid circular imports
	Schema           any
	SchemaPath       string
	ValidateOnLoad   bool
	StrictValidation bool
	FreezeOnLoad     bool
	OnError          ErrorPolicy
	DebugMode        bool
	Logger           *slog.Logger

	// selfConfigSources holds declarative `.confii.*` sources until the
	// active environment has been resolved. Most source types do not depend
	// on the environment, but `environment_files` does; deferring the entire
	// ordered list preserves source precedence when the two kinds are mixed.
	selfConfigSources []map[string]any
	// SecretHook is a context-aware hook registered on the Config's hook
	// processor at construction time so secret placeholders (for example
	// ${secret:db/password}) are resolved during value access. It is the
	// option-level wiring path (G04) parallel to the post-construction
	// pattern of cfg.HookProcessor().RegisterGlobalHookCtx(...). Self-
	// config `secrets` declarations populate this field with a built-in
	// env-store-backed hook when no explicit [WithSecretHook] is supplied;
	// explicit wins.
	SecretHook hook.FuncCtx

	// Tracks which fields were explicitly set by user options.
	// Used to implement priority: explicit > self-config > built-in default.
	explicitlySet map[string]bool
}

func defaultOptions() options {
	return options{
		Env:            "",
		UseEnvExpander: true,
		UseTypeCasting: true,
		DeepMerge:      true,
		OnError:        ErrorPolicyRaise,
		Logger:         slog.Default(),
		explicitlySet:  make(map[string]bool),
	}
}

// isSet returns true if the given option was explicitly set by the user.
func (o *options) isSet(key string) bool {
	return o.explicitlySet[key]
}

// Option configures a Config instance.
type Option func(*options)

// WithWorkingDir sets the base directory used for self-config discovery
// and for resolving relative `_include` / `_defaults` paths in the
// composer.
//
// G06 (Wave 22): prior to this option [confii.New] always read the
// self-config file from the process CWD ("." literally) and instantiated
// the composer with the same hard-coded "." basePath. That coupled the
// library's discovery semantics to whatever directory the host process
// happened to be in, which surprised callers who used Confii from
// long-running daemons, tests, or sub-commands that chdir mid-run. With
// [WithWorkingDir] the caller picks the discovery base explicitly:
//
//   - selfconfig.Read receives this dir, so confii.{yaml,json,toml} and
//     .confii.* files are searched under the supplied directory (with
//     ~/.config/confii/ XDG fallback unchanged).
//   - The selfconfig package caches results keyed by filepath.Abs(dir),
//     so two Config instances created with different WithWorkingDir
//     values do NOT share self-config state.
//   - compose.New is initialized with this dir as basePath, so a YAML
//     containing `_include: ./other.yaml` resolves `./other.yaml`
//     relative to dir, not relative to the process CWD.
//
// Empty string is treated as "." (the pre-G06 default) for backward
// compatibility — existing code that does not call WithWorkingDir behaves
// exactly as it did before Wave 22.
func WithWorkingDir(dir string) Option {
	return func(o *options) {
		o.WorkingDir = dir
		o.explicitlySet["working_dir"] = true
	}
}

// WithEnv sets the active environment name (e.g., "production").
//
// G02: an explicit [WithEnv] takes priority over any [WithEnvSwitcher]
// OS-variable lookup or [selfconfig] env-switcher hint, matching the
// documented precedence "explicit `WithEnv()` > `WithEnvSwitcher()` OS
// variable > self-config file `default_environment` > empty string".
//
// To enforce that precedence regardless of the order in which options are
// applied, this option also clears any previously-set EnvSwitcher field
// and marks "env_switcher" as explicitly handled so [applySelfConfig]
// will not later repopulate it. The semantics: when the caller has named
// an environment by hand, no OS-variable lookup should second-guess that
// choice. Precedence resolution is intentionally centralized in the
// [github.com/confiify/confii-go/envhandler] package
// (see [envhandler.ResolveEnv]) so it can be unit-tested in isolation.
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
// G02: this option is a no-op when [WithEnv] has already been applied,
// because explicit code-level environment selection always outranks an
// OS-variable lookup. The order in which options appear in the [New]
// argument list does not matter — [WithEnv] wins regardless of order.
func WithEnvSwitcher(envVar string) Option {
	return func(o *options) {
		// G02: explicit WithEnv always wins. Skip recording the
		// env-switcher hint so the resolver does not later overwrite
		// the explicitly chosen env with an OS-variable value.
		if o.explicitlySet["env"] {
			return
		}
		o.EnvSwitcher = envVar
		o.explicitlySet["env_switcher"] = true
	}
}

// WithLoaders sets the ordered list of loaders. Later loaders override earlier ones.
func WithLoaders(loaders ...Loader) Option {
	return func(o *options) { o.Loaders = loaders; o.explicitlySet["loaders"] = true }
}

// WithDynamicReloading enables file watching for automatic reload on change.
//
// G14: this option is mutually exclusive with [WithFreezeOnLoad](true)
// — combining them is rejected at [confii.New] time with a typed
// [*ConfigError] wrapping [ErrConfigLoad], because a frozen Config
// refuses every reload and the watcher would emit only failure logs.
// Pick one: either keep the Config mutable with dynamic reloading, or
// freeze it for safety and rely on process restarts for changes.
func WithDynamicReloading(v bool) Option {
	return func(o *options) { o.DynamicReloading = v; o.explicitlySet["dynamic_reloading"] = true }
}

// WithEnvExpander enables or disables ${VAR} expansion in string values.
func WithEnvExpander(v bool) Option {
	return func(o *options) { o.UseEnvExpander = v; o.explicitlySet["use_env_expander"] = true }
}

// WithTypeCasting enables or disables automatic type casting of string values.
func WithTypeCasting(v bool) Option {
	return func(o *options) { o.UseTypeCasting = v; o.explicitlySet["use_type_casting"] = true }
}

// WithDeepMerge enables or disables deep merging of nested maps.
func WithDeepMerge(v bool) Option {
	return func(o *options) { o.DeepMerge = v; o.explicitlySet["deep_merge"] = true }
}

// WithMergeStrategyOption sets the default merge strategy.
func WithMergeStrategyOption(s MergeStrategy) Option {
	return func(o *options) { o.MergeStrategy = &s; o.explicitlySet["merge_strategy"] = true }
}

// WithMergeStrategyMap sets per-path merge strategy overrides. The map
// is honored on its own — supplying it without also calling
// [WithMergeStrategyOption] is sufficient to activate the advanced
// merger. When no global default strategy is provided, the merger
// falls back to deep-merge for paths not covered by the map.
func WithMergeStrategyMap(m map[string]MergeStrategy) Option {
	return func(o *options) { o.MergeStrategyMap = m; o.explicitlySet["merge_strategy_map"] = true }
}

// WithEnvPrefix auto-adds an environment-variable loader for this prefix.
//
// The recorded prefix is used by the sysenv fallback path
// ([WithSysenvFallback]) to resolve missing keys via OS environment
// variables. As of Wave 13 G03, [WithEnvPrefix] also appends an
// auto-installed environment loader equivalent to
// `loader.NewEnvironment(prefix)` to the loader chain so that environment
// variables participate in the normal load+merge pipeline (not only the
// after-the-fact sysenv fallback resolution at Get-time). The loader uses
// the canonical "__" nesting separator: e.g., `APP_DB__HOST=postgres`
// becomes `db.host`. The prefix is uppercased to match
// [loader.EnvironmentLoader] semantics.
//
// Double-application is avoided. If the user has already supplied their
// own `loader.NewEnvironment(prefix)` via [WithLoaders] before calling
// [WithEnvPrefix] (with a matching uppercased prefix), the auto-loader
// is not appended; the user's loader keeps its position in the chain.
// Detection uses Loader.Source() identity (`environment:<UPPER_PREFIX>`).
func WithEnvPrefix(prefix string) Option {
	return func(o *options) {
		o.EnvPrefix = prefix
		o.explicitlySet["env_prefix"] = true

		if prefix == "" {
			return
		}

		// G03: append the environment loader to the load pipeline so
		// env vars are merged in like any other source. Match the
		// canonical loader.EnvironmentLoader source identifier so we
		// can dedupe against an explicitly-supplied env loader.
		upper := strings.ToUpper(prefix)
		wantSource := "environment:" + upper
		for _, existing := range o.Loaders {
			if existing != nil && existing.Source() == wantSource {
				// User already supplied an equivalent loader.
				// Skip auto-append to avoid double-application.
				return
			}
		}
		o.Loaders = append(o.Loaders, &envPrefixAutoLoader{prefix: upper})
	}
}

// WithSysenvFallback enables fallback to system env vars for missing keys.
func WithSysenvFallback(v bool) Option {
	return func(o *options) { o.SysenvFallback = v; o.explicitlySet["sysenv_fallback"] = true }
}

// WithSchema sets the validation schema (struct type or JSON schema dict).
func WithSchema(schema any) Option {
	return func(o *options) { o.Schema = schema; o.explicitlySet["schema"] = true }
}

// WithSchemaPath sets the path to a JSON Schema file.
func WithSchemaPath(path string) Option {
	return func(o *options) { o.SchemaPath = path; o.explicitlySet["schema_path"] = true }
}

// WithValidateOnLoad validates configuration immediately after loading.
func WithValidateOnLoad(v bool) Option {
	return func(o *options) { o.ValidateOnLoad = v; o.explicitlySet["validate_on_load"] = true }
}

// WithStrictValidation raises errors on validation failure instead of warning.
func WithStrictValidation(v bool) Option {
	return func(o *options) { o.StrictValidation = v; o.explicitlySet["strict_validation"] = true }
}

// WithFreezeOnLoad freezes the config immediately after the initial load
// succeeds. A frozen Config rejects every mutation API ([Config.Set],
// [Config.Reload], [Config.Extend], [Config.RollbackToVersion]) with
// [ErrConfigFrozen].
//
// G14: this option is mutually exclusive with [WithDynamicReloading](true)
// — combining them is rejected at [confii.New] time with a typed
// [*ConfigError]. A frozen Config under a watcher would emit only
// "config is frozen" reload errors. Use one or the other.
func WithFreezeOnLoad(v bool) Option {
	return func(o *options) { o.FreezeOnLoad = v; o.explicitlySet["freeze_on_load"] = true }
}

// WithOnError sets the error-handling policy applied when a loader's
// Load call fails or when composition (`_include` / `_defaults`)
// processing returns an error. The three behaviors are strictly distinct
// after G07:
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

// WithDebugMode enables detailed source tracking.
func WithDebugMode(v bool) Option {
	return func(o *options) { o.DebugMode = v; o.explicitlySet["debug_mode"] = true }
}

// WithLogger sets the logger for the config instance.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.Logger = l; o.explicitlySet["logger"] = true }
}

// WithSecretHook registers a context-aware secret-resolution hook on the
// Config's hook processor at construction time. The hook is invoked for
// every accessed value (matching the [hook.Processor.RegisterGlobalHookCtx]
// contract) so placeholders such as ${secret:KEY} can be substituted with
// the resolved secret. Errors returned by the hook propagate to the caller
// of [Config.GetCtx], honoring the standard hook error contract.
//
// G04: this option is the explicit-wins counterpart to a self-config
// `secrets:` declaration. When both are supplied, the explicit hook wins
// and the self-config-derived hook is dropped (consistent with the rest
// of the explicit > self-config priority).
func WithSecretHook(h hook.FuncCtx) Option {
	return func(o *options) { o.SecretHook = h; o.explicitlySet["secret_hook"] = true }
}
