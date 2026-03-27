package confii

import "log/slog"

// ErrorPolicy defines how errors are handled during loading.
type ErrorPolicy string

const (
	ErrorPolicyRaise  ErrorPolicy = "raise"
	ErrorPolicyWarn   ErrorPolicy = "warn"
	ErrorPolicyIgnore ErrorPolicy = "ignore"
)

// options holds all resolved configuration options.
// Fields use pointers for booleans/strings where we need to distinguish
// "not set" from "set to zero value" (for 3-tier priority resolution).
type options struct {
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

// WithEnv sets the active environment name (e.g., "production").
func WithEnv(env string) Option {
	return func(o *options) { o.Env = env; o.explicitlySet["env"] = true }
}

// WithEnvSwitcher sets the OS environment variable name whose value selects the active environment.
func WithEnvSwitcher(envVar string) Option {
	return func(o *options) { o.EnvSwitcher = envVar; o.explicitlySet["env_switcher"] = true }
}

// WithLoaders sets the ordered list of loaders. Later loaders override earlier ones.
func WithLoaders(loaders ...Loader) Option {
	return func(o *options) { o.Loaders = loaders; o.explicitlySet["loaders"] = true }
}

// WithDynamicReloading enables file watching for automatic reload on change.
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

// WithMergeStrategyMap sets per-path merge strategy overrides.
func WithMergeStrategyMap(m map[string]MergeStrategy) Option {
	return func(o *options) { o.MergeStrategyMap = m; o.explicitlySet["merge_strategy_map"] = true }
}

// WithEnvPrefix auto-adds an EnvironmentLoader with this prefix.
func WithEnvPrefix(prefix string) Option {
	return func(o *options) { o.EnvPrefix = prefix; o.explicitlySet["env_prefix"] = true }
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

// WithFreezeOnLoad freezes the config after initialization.
func WithFreezeOnLoad(v bool) Option {
	return func(o *options) { o.FreezeOnLoad = v; o.explicitlySet["freeze_on_load"] = true }
}

// WithOnError sets the error handling policy.
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
