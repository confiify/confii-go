package confii

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/confiify/confii-go/compose"
	"github.com/confiify/confii-go/diff"
	"github.com/confiify/confii-go/envhandler"
	"github.com/confiify/confii-go/hook"
	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/internal/formatparse"
	"github.com/confiify/confii-go/internal/sourcekind"
	"github.com/confiify/confii-go/internal/typecoerce"
	"github.com/confiify/confii-go/merge"
	"github.com/confiify/confii-go/observe"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/confiify/confii-go/sourcetrack"
	"github.com/confiify/confii-go/validate"
	"github.com/confiify/confii-go/watch"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
)

// Config is the central configuration manager. It loads, merges, validates,
// and serves configuration values from multiple sources (files, environment
// variables, remote stores). The type parameter T defines the strongly-typed
// model returned by [Config.Model]. All public methods are safe for concurrent use.
type Config[T any] struct {
	mu sync.RWMutex

	// Core state.
	envConfig    map[string]any
	mergedConfig map[string]any
	frozen       bool
	env          string

	// Collaborators.
	loaders       []Loader
	merger        Merger
	hookProcessor *hook.Processor
	envHandler    *envhandler.Handler
	sourceTracker *sourcetrack.Tracker
	fileTracker   *sourcetrack.FileTracker
	composer      *compose.Composer

	// Observability (nil until enabled).
	observer     *observe.Metrics
	eventEmitter *observe.EventEmitter
	versionMgr   *observe.VersionManager
	watcher      *watch.Watcher

	// Settings.
	opts   options
	logger *slog.Logger

	// Typed model cache.
	validatedModel *T

	// Compiled JSON Schema validator (G01).
	//
	// Populated lazily by [resolveSchemaValidator] from either a
	// map[string]any schema passed via [WithSchema] or a file path
	// passed via [WithSchemaPath]. nil when the caller supplied a
	// struct-shaped schema (struct validation flows through Typed()
	// instead) or no schema at all. Reused across [Config.New] and
	// [Config.Reload] so the schema file is read only once.
	jsonSchema *validate.JSONSchemaValidator

	// Change callbacks.
	changeCallbacks []func(key string, oldVal, newVal any)

	// Override stack. Each call to [Config.Override] pushes a frame;
	// the returned restore closure removes its own frame regardless of
	// stack position. When the stack is non-empty the live envConfig /
	// mergedConfig / sourceTracker are derived by replaying remaining
	// frames onto the captured base. The base is captured when the
	// stack transitions empty → non-empty, so a fully-drained stack
	// returns the Config to its pre-Override state atomically. See
	// [Config.Override] for the LIFO composability contract.
	overrideStack       []*overrideFrame
	overrideIDCounter   uint64
	overrideBaseEnv     map[string]any
	overrideBaseMerged  map[string]any
	overrideBaseTracker sourcetrack.Snapshot
	overrideBaseFrozen  bool
}

// overrideFrame is a single entry in [Config.overrideStack]. The
// applied flag is flipped on the first restore invocation, so a
// second call is a no-op.
type overrideFrame struct {
	id      uint64
	payload map[string]any
	applied bool
}

// New creates a new Config instance, loading and merging all sources.
//
// Initialization follows the priority: explicit argument > self-config file > built-in default.
//
// Some option combinations are mutually exclusive and rejected here with
// a typed [*ConfigError] before any sources are loaded. Currently:
//
//   - [WithFreezeOnLoad](true) + [WithDynamicReloading](true): a frozen
//     Config refuses Reload, so a watcher on a frozen Config would
//     produce only a stream of "config is frozen" errors. Pick one
//     (G14).
func New[T any](ctx context.Context, cfgOpts ...Option) (*Config[T], error) {
	opts := defaultOptions()
	for _, fn := range cfgOpts {
		fn(&opts)
	}

	// Step 1: Read self-configuration.
	//
	// G07: a typed *ConfigError returned by applySelfConfig (for example,
	// from an invalid on_error string) is surfaced to the caller instead
	// of being downgraded to a warning, so operators see misconfiguration
	// loudly. Other failures — file I/O, malformed self-config — remain
	// best-effort warnings to preserve historical behavior for the
	// graceful-absence case.
	if err := applySelfConfig(&opts); err != nil {
		var ce *ConfigError
		if errors.As(err, &ce) {
			return nil, err
		}
		opts.Logger.Warn("failed to read self-config", slog.String("error", err.Error()))
	}

	// Step 1a: Reject mutually exclusive option combinations.
	//
	// G14: WithFreezeOnLoad and WithDynamicReloading were silently
	// compatible at the option level even though FreezeOnLoad makes
	// every watcher-driven Reload fail with ErrConfigFrozen. Rather
	// than special-case the watcher to bypass the freeze (which would
	// make the freeze leaky and surprising), we fail fast at construction
	// time so the operator sees the conflict at deploy/start, not as a
	// silent no-op when files change in production.
	if opts.FreezeOnLoad && opts.DynamicReloading {
		return nil, &ConfigError{
			Op: "New",
			Err: fmt.Errorf(
				"%w: WithFreezeOnLoad(true) and WithDynamicReloading(true) are mutually exclusive: "+
					"a frozen Config refuses Reload, so the watcher would produce only "+
					"%q errors on every file change",
				ErrConfigLoad, "config is frozen",
			),
		}
	}

	// Step 2: Resolve environment.
	if opts.EnvSwitcher != "" {
		if envVal := os.Getenv(opts.EnvSwitcher); envVal != "" {
			opts.Env = envVal
		}
	}

	// Step 3: Set up merger.
	// An AdvancedMerger is required whenever the caller supplies a default
	// merge strategy OR a non-empty per-path strategy map. Previously the
	// per-path map was silently dropped when WithMergeStrategyOption was
	// not also set, which violated the documented behavior of
	// WithMergeStrategyMap (G16).
	var m Merger = merge.NewDefault(opts.DeepMerge)
	if opts.MergeStrategy != nil || len(opts.MergeStrategyMap) > 0 {
		defaultStrategy := merge.DeepMergeStrategy
		if opts.MergeStrategy != nil {
			defaultStrategy = *opts.MergeStrategy
		}
		m = merge.NewAdvanced(defaultStrategy, opts.MergeStrategyMap)
	}

	// G01: compile any configured JSON Schema (inline map or file path)
	// up front so that compile-time errors surface before sources are
	// loaded — failing fast on a malformed schema is strictly better
	// than loading every source and only then discovering the validator
	// itself is broken. The compiled validator is reused across
	// [Config.Reload] and [Config.Extend] via c.jsonSchema; the file is
	// read at most once per Config lifetime.
	jsonSchema, err := resolveSchemaValidator(&opts)
	if err != nil {
		return nil, err
	}

	// G06 (Wave 22): seed the composer with the option-supplied WorkingDir
	// so `_include: ./other.yaml` resolves relative to the caller's chosen
	// base, not the process CWD. Empty falls back to "." (pre-G06 default)
	// inside compose.New, preserving backward compatibility.
	composerBase := opts.WorkingDir
	if composerBase == "" {
		composerBase = "."
	}

	c := &Config[T]{
		env:           opts.Env,
		loaders:       opts.Loaders,
		merger:        m,
		hookProcessor: hook.NewProcessor(),
		envHandler:    envhandler.New(opts.Logger),
		sourceTracker: sourcetrack.NewTracker(opts.DebugMode),
		fileTracker:   sourcetrack.NewFileTracker(),
		composer:      compose.New(composerBase, compose.WithMerger(m)),
		opts:          opts,
		logger:        opts.Logger,
		jsonSchema:    jsonSchema,
	}

	// Step 4: Register default hooks in order: env expander, then type casting.
	if opts.UseEnvExpander {
		c.hookProcessor.RegisterGlobalHook(hook.NewEnvExpanderHook())
	}
	if opts.UseTypeCasting {
		c.hookProcessor.RegisterGlobalHook(hook.NewTypeCastHook())
	}
	// G04: register the option-level secret-resolution hook (either
	// supplied explicitly via [WithSecretHook] or synthesised from a
	// self-config `secrets:` block by [applySelfConfig]). Registered
	// after the built-in expander/typecast hooks so secret substitution
	// runs once env-expansion and type-coercion have shaped the value
	// — matching the order recommended for post-construction
	// HookProcessor().RegisterGlobalHookCtx(resolver.HookCtx()) wiring.
	if opts.SecretHook != nil {
		c.hookProcessor.RegisterGlobalHookCtx(opts.SecretHook)
	}

	// Step 5: Load all configurations.
	if err := c.load(ctx); err != nil {
		return nil, err
	}

	// Step 6: Validate on load.
	//
	// G01: validate-on-load now honors JSON Schema validators wired via
	// [WithSchema](map) and [WithSchemaPath] in addition to the legacy
	// struct path that flows through [Config.Typed]. The dispatch is
	// centralized in [runValidateOnLoad] so [Config.Reload] and
	// [Config.Extend] use the same logic.
	if err := c.runValidateOnLoad(); err != nil {
		return nil, err
	}

	// Step 7: Freeze if requested.
	if opts.FreezeOnLoad {
		c.frozen = true
	}

	// Step 8: Start file watcher if requested.
	if opts.DynamicReloading {
		c.startWatching()
	}

	return c, nil
}

// load loads and merges all configurations with source tracking and composition.
//
// Errors from individual loaders and from the composition pass are routed
// through c.opts.OnError so that the three [ErrorPolicy] values behave
// distinctly (G07):
//
//   - [ErrorPolicyRaise]: the first error is returned immediately; New /
//     Reload propagate it to the caller.
//   - [ErrorPolicyWarn]: the error is logged at warn level via c.logger
//     and the source is skipped (loaders) or its raw data is used
//     (composer).
//   - [ErrorPolicyIgnore]: the error is silently swallowed with no log
//     record at all — distinct from Warn, which always logs.
func (c *Config[T]) load(ctx context.Context) error {
	var configs []map[string]any

	for _, l := range c.loaders {
		data, err := l.Load(ctx)
		if err != nil {
			switch c.opts.OnError {
			case ErrorPolicyRaise:
				return err
			case ErrorPolicyWarn:
				c.logger.Warn(
					"loader error",
					slog.String("source", l.Source()),
					slog.String("error", err.Error()),
				)
			case ErrorPolicyIgnore:
				// Silent: distinct from Warn, do not emit a log record.
			default:
				// Unknown policy values are treated as Raise so that
				// misconfiguration surfaces loudly rather than degrading
				// silently to Warn-or-Ignore behavior.
				return err
			}
			continue
		}
		if data == nil {
			continue
		}

		// Process composition directives (_include, _defaults). Errors
		// here used to be swallowed and the raw (un-composed) data was
		// loaded regardless of policy. Post-G07, composition errors flow
		// through the same OnError dispatch as loader errors.
		composed, err := c.composer.Compose(data, l.Source())
		if err != nil {
			switch c.opts.OnError {
			case ErrorPolicyRaise:
				return err
			case ErrorPolicyWarn:
				c.logger.Warn(
					"composition error",
					slog.String("source", l.Source()),
					slog.String("error", err.Error()),
				)
				composed = data
			case ErrorPolicyIgnore:
				composed = data
			default:
				return err
			}
		}

		// G08: Track each loader's RESOLVED contribution against the loader
		// source so introspection reports resolved key paths
		// (e.g. "database.host" not "production.database.host") AND attributes
		// every key to the file/loader it actually came from. Pre-G08 this
		// path tracked the raw composed data and then issued a second
		// TrackConfig pass with the synthetic source string "(resolved)" that
		// overwrote the loader source for every env-resolved key. Tracking
		// the resolved-per-loader output here mirrors the pattern already
		// used by Config.Extend (see config.go:1325-1329) and is Option A
		// from the G08 closure plan: skip the post-resolve retrack entirely
		// because the loader source is already the right answer.
		loaderType := loaderTypeName(l)
		resolvedLayer := c.envHandler.Resolve(composed, c.env)
		c.sourceTracker.TrackConfig(resolvedLayer, l.Source(), loaderType, c.env, "")

		// Track file for incremental reload.
		_ = c.fileTracker.Track(l.Source())

		configs = append(configs, composed)
	}

	c.mergedConfig = merge.MergeAll(c.merger, configs...)
	c.envConfig = c.envHandler.Resolve(c.mergedConfig, c.env)

	return nil
}

// ---------------------------------------------------------------------------
// Access methods
// ---------------------------------------------------------------------------

// Get retrieves a value by dot-separated key path. Hooks are applied
// uniformly: scalar leaves run through the hook pipeline (env expansion,
// secret resolution, type casting, custom hooks), and when keyPath
// addresses a sub-tree the returned map is a hook-applied deep copy
// whose every leaf has been processed by the same pipeline.
//
// Get is equivalent to calling [Config.GetCtx] with [context.Background].
// Legacy [hook.Func] hooks have no way to surface errors and run as
// before. Context-aware [hook.FuncCtx] hooks (for example, a
// [secret.Resolver] registered via [Resolver.HookCtx]) execute under
// [context.Background] and any error they return is propagated back to
// the caller of Get. Callers that need to honor per-request deadlines
// or thread caller context through to remote stores should use
// [Config.GetCtx] directly.
//
// G11: prior to Wave 8, only scalar Get applied hooks; whole-map Get,
// [Config.Typed], [Config.ToDict], and [Config.Export] returned raw
// (unhooked) values. The pipeline is now applied uniformly across every
// access mode so that a service calling cfg.Get("db.password") and one
// calling cfg.Typed[Config]() see the same effective value.
func (c *Config[T]) Get(keyPath string) (any, error) {
	return c.GetCtx(context.Background(), keyPath)
}

// GetCtx retrieves a value by dot-separated key path, threading ctx through
// any registered context-aware hooks.
//
// The supplied ctx is passed to every [hook.FuncCtx] registered on the
// processor (see [hook.Processor.RegisterGlobalHookCtx]). If a hook
// returns an error — for example, a [secret.Resolver] configured with
// [secret.WithResolverFailOnMissing](true) when a referenced secret is
// not found — that error is returned to the caller and the partially
// resolved value is discarded. Hooks of the legacy [hook.Func] signature
// continue to run unchanged and cannot raise errors.
//
// When keyPath resolves to a sub-tree (a nested map) the returned value
// is a hook-applied deep copy: every leaf in the returned map has been
// passed through the hook pipeline with its absolute key path. Mutating
// the returned map is safe and does not affect live config state.
//
// G10 (Wave 12): the read-side deep-copy contract is now symmetric with
// the write-side contract closed in Wave 11 (Set deep-copies inputs via
// dictutil.DeepCopyValue). Whole-map Get already flowed through
// applyHooksRecursive, which produces a deep copy. For the scalar /
// slice leaf path this method now defensively deep-copies the
// hook-processor output via dictutil.DeepCopyValue before returning, so
// a caller cannot mutate a returned []any (or a map embedded in a hook
// result) and have those mutations leak back into envConfig — which
// would otherwise let users bypass Freeze and break the documented
// thread-safety claim.
func (c *Config[T]) GetCtx(ctx context.Context, keyPath string) (any, error) {
	c.mu.RLock()
	val, ok := dictutil.GetNested(c.envConfig, keyPath)
	if !ok {
		if c.opts.SysenvFallback {
			if envVal, found := c.lookupSysenv(keyPath); found {
				c.mu.RUnlock()
				processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, envVal)
				if err != nil {
					return nil, err
				}
				// Sysenv values are strings, but a hook may produce a
				// reference type. Defensive copy keeps the contract
				// uniform.
				return dictutil.DeepCopyValue(processed), nil
			}
		}
		availableKeys := dictutil.FlatKeys(c.envConfig)
		c.mu.RUnlock()
		return nil, NewNotFoundError(keyPath, availableKeys)
	}
	// Snapshot the selected value while it is protected, then release the
	// Config lock before running user hooks. Hooks may perform I/O or call
	// back into Config.Set/Get; executing them under c.mu would block writers
	// and makes a re-entrant write deadlock.
	val = dictutil.DeepCopyValue(val)
	c.mu.RUnlock()

	if subMap, isMap := val.(map[string]any); isMap {
		// G11: whole-map Get used to bypass hooks entirely. Apply the
		// hook pipeline to every leaf in a deep copy so the caller sees
		// resolved (env-expanded, secret-substituted, type-cast) values
		// regardless of whether they fetched a scalar or a sub-tree.
		// applyHooksRecursive already produces a deep copy (G11 +
		// Wave 12 G10 read-side deep-copy contract).
		return c.applyHooksRecursive(ctx, keyPath, subMap)
	}
	if subSlice, isSlice := val.([]any); isSlice {
		// G10 (Wave 12): a leaf []any in envConfig was previously
		// returned to the caller as a live reference — mutation of
		// any element would alias into Config state, breaking Freeze
		// and racing against concurrent writers. Apply the hook
		// pipeline element-by-element (matching ToDict / Export) and
		// return the resulting fresh slice, which is by construction
		// a deep copy.
		return c.applyHooksToSlice(ctx, keyPath, subSlice)
	}
	processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, val)
	if err != nil {
		return nil, err
	}
	// G10 (Wave 12): scalar leaves are typically immutable (string,
	// int, bool, nil), but a custom hook is free to materialize a
	// map/slice. Run the result through DeepCopyValue so the contract
	// holds uniformly: nothing returned from Get aliases live state.
	return dictutil.DeepCopyValue(processed), nil
}

// GetOr retrieves a value by key path, returning the default if not found.
func (c *Config[T]) GetOr(keyPath string, defaultVal any) any {
	val, err := c.Get(keyPath)
	if err != nil {
		return defaultVal
	}
	return val
}

// GetString retrieves a string value by key path.
func (c *Config[T]) GetString(keyPath string) (string, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return "", err
	}
	if s, ok := val.(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", val), nil
}

// GetStringOr retrieves a string value, returning the default if not found.
func (c *Config[T]) GetStringOr(keyPath, defaultVal string) string {
	s, err := c.GetString(keyPath)
	if err != nil {
		return defaultVal
	}
	return s
}

// GetInt retrieves an int value by key path.
//
// G30 (Wave 21) — behavior tightening (no API signature change). For a
// stored float64 (the natural type produced by JSON / YAML decoding of
// numeric literals), GetInt now requires the value to have a zero
// fractional part (i.e. math.Trunc(v) == v). When the fractional part is
// non-zero the call returns a typed *ConfigError wrapping
// [ErrConfigInvalid] instead of silently truncating: pre-Wave 21
// behavior turned `cfg.Set("port", 3.14); cfg.GetInt("port")` into the
// surprise return value (3, nil), which masked configuration bugs.
// Integer-valued floats (3.0, -7.0, 1e6) continue to convert cleanly.
// The float64 → int conversion also guards against int-range overflow.
func (c *Config[T]) GetInt(keyPath string) (int, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, &ConfigError{
				Op:  "GetInt",
				Key: keyPath,
				Err: fmt.Errorf("%w: cannot convert non-finite float64 (%v) to int", ErrConfigInvalid, v),
			}
		}
		if math.Trunc(v) != v {
			return 0, &ConfigError{
				Op:  "GetInt",
				Key: keyPath,
				Err: fmt.Errorf("%w: float64 value %v has non-zero fractional part; refusing to truncate to int", ErrConfigInvalid, v),
			}
		}
		// Range-check before narrowing so we don't silently wrap on
		// architectures where int is 32 bits or values that exceed
		// int64 range when cast.
		if v > float64(math.MaxInt) || v < float64(math.MinInt) {
			return 0, &ConfigError{
				Op:  "GetInt",
				Key: keyPath,
				Err: fmt.Errorf("%w: float64 value %v overflows int", ErrConfigInvalid, v),
			}
		}
		return int(v), nil
	default:
		return 0, &ConfigError{Op: "GetInt", Key: keyPath, Err: fmt.Errorf("cannot convert %T to int", val)}
	}
}

// GetIntOr retrieves an int value, returning the default if not found.
func (c *Config[T]) GetIntOr(keyPath string, defaultVal int) int {
	v, err := c.GetInt(keyPath)
	if err != nil {
		return defaultVal
	}
	return v
}

// GetBool retrieves a bool value by key path.
//
// In addition to a native bool, GetBool accepts the canonical string
// boolean spellings documented in docs/access.md and produced by the
// type-casting hook (see [WithTypeCasting]):
//
//   - true:  "true",  "1", "yes", "on"
//   - false: "false", "0", "no",  "off"
//
// Matching is case-insensitive and surrounding whitespace is trimmed.
// Numeric forms 0 and 1 (as int / int64 / float64) also coerce, so a
// JSON value `1` and a YAML value `true` are treated equivalently. Any
// other value returns a typed *ConfigError.
//
// G30 (Wave 21) — behavior tightening (no API signature change). Prior
// to Wave 21, GetBool only accepted a native Go bool, so a stored string
// "1" or "yes" — which docs/access.md and the type-casting hook docs
// both claim are valid bool forms — failed with "cannot convert string
// to bool". The accepted-form set now matches the documented contract.
func (c *Config[T]) GetBool(keyPath string) (bool, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return false, err
	}
	switch v := val.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		}
		return false, &ConfigError{
			Op:  "GetBool",
			Key: keyPath,
			Err: fmt.Errorf("cannot convert string %q to bool (accepted: true/false, 1/0, yes/no, on/off)", v),
		}
	case int:
		switch v {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	case int64:
		switch v {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	case float64:
		switch v {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	}
	return false, &ConfigError{Op: "GetBool", Key: keyPath, Err: fmt.Errorf("cannot convert %T to bool", val)}
}

// GetBoolOr retrieves a bool value, returning the default if not found.
func (c *Config[T]) GetBoolOr(keyPath string, defaultVal bool) bool {
	v, err := c.GetBool(keyPath)
	if err != nil {
		return defaultVal
	}
	return v
}

// GetFloat64 retrieves a float64 value by key path.
func (c *Config[T]) GetFloat64(keyPath string) (float64, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	default:
		return 0, &ConfigError{Op: "GetFloat64", Key: keyPath, Err: fmt.Errorf("cannot convert %T to float64", val)}
	}
}

// MustGet retrieves a value and panics on error. Intended for tests.
func (c *Config[T]) MustGet(keyPath string) any {
	val, err := c.Get(keyPath)
	if err != nil {
		panic(err)
	}
	return val
}

// Has reports whether keyPath is reachable through any resolution path
// that [Config.Get] would consult.
//
// Specifically Has returns true when:
//
//   - keyPath is present in the loaded/merged config map; OR
//   - sysenv fallback is enabled (the default; see [WithSysenvFallback])
//     AND a process-environment variable named after keyPath (uppercased,
//     dots replaced with underscores, optionally prefixed with the
//     configured [WithEnvPrefix]) is set.
//
// G30 (Wave 21) — behavior tightening (no API signature change). Prior
// to Wave 21, Has consulted only the in-memory config map and ignored
// sysenv fallback, so a caller could observe Has("db.password") == false
// while Get("db.password") returned a real value resolved via
// $APP_DB_PASSWORD. The two helpers now agree: if Has reports true, Get
// will not return ErrConfigNotFound (modulo concurrent mutation); if Has
// reports false, Get returns ErrConfigNotFound.
func (c *Config[T]) Has(keyPath string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if dictutil.HasNested(c.envConfig, keyPath) {
		return true
	}
	if c.opts.SysenvFallback {
		if _, found := c.lookupSysenv(keyPath); found {
			return true
		}
	}
	return false
}

// SetOption is a functional option that configures the behavior of [Config.Set].
type SetOption func(*setOpts)
type setOpts struct{ allowOverride bool }

// WithOverride allows or prevents overwriting existing keys. Default: true.
func WithOverride(v bool) SetOption {
	return func(o *setOpts) { o.allowOverride = v }
}

// Set sets a value by dot-separated key path. Thread-safe, respects frozen state.
// Pass WithOverride(false) to raise an error if the key already exists.
//
// G12 contract (Wave 11):
//
//   - Source-tracking parity. A successful Set records the new value
//     against the synthetic source "runtime" so [Config.Explain] and
//     friends report runtime mutations alongside file/env-loaded keys.
//     The runtime tracking is overwritten by a subsequent Reload.
//
//   - Error propagation. dictutil.SetNested returns a *PathError when
//     a key path traverses through a non-map intermediate (for example
//     setting "service.host" when "service" is already bound to a
//     scalar). Pre-G12 this error was silently swallowed; Set now
//     surfaces it as a typed [*ConfigError] wrapping [ErrConfigInvalid]
//     and leaves both envConfig and mergedConfig unmutated.
//
//   - Defensive deep copy (F-G10-SetInputAlias). The caller's value is
//     deep-copied via [dictutil.DeepCopyValue] before storage so a
//     subsequent caller-side mutation of a passed map/slice cannot
//     bleed into Config state. This mirrors the read-side deep-copy
//     contract introduced in G11.
//
//   - Change-event parity (F-G12-Set-CallbackSilence). Following the
//     G13 lock-release-then-callback pattern (see [Config.Reload],
//     [Config.Extend], [Config.Override]), a successful Set fires
//     OnChange callbacks AFTER c.mu has been released so a callback
//     that calls back into the Config cannot deadlock. Callbacks see
//     the same union-iteration deletion contract: setting a key to a
//     new non-equal value emits (oldVal, newVal); a key that was
//     present and is now bound to a value that flatten cannot reach
//     (e.g. nil) emits (oldVal, nil).
//
//   - Observability parity (G21 residual). When metrics or events are
//     enabled, a successful Set emits "set" / "change" events and
//     [observe.Metrics.RecordSet] / [observe.Metrics.RecordChange];
//     a failed Set emits "set_failed" and [observe.Metrics.RecordSetFailed].
//
//   - Rollback fidelity (F-Set-RollbackFidelity, Wave 17). Pre-Wave 17
//     a SetNested *PathError on c.mergedConfig rolled envConfig back via
//     dictutil.Unflatten(Flatten(envConfig)), which collapses non-string
//     scalar types (int -> float64, time.Duration -> int64), drops
//     nil-leaf distinctions, and cannot reconstruct sub-tree shape. The
//     rollback is now a structural Snapshot+Restore of envConfig,
//     mergedConfig, and the source tracker — the same idiom used by
//     [Config.Reload] (Wave 7 G14) and [Config.Override] (Wave 16). The
//     pre-Wave 17 code also skipped rollback on the FIRST SetNested
//     failure even though that call may have inserted intermediate maps
//     before erroring; the Wave 17 path rolls envConfig back on both
//     SetNested failures so a rejected Set leaves no observable trace.
//
//   - fileTracker divergence is intentional (G12-NewKey-Loaders, Wave 19).
//     Set claims the synthetic source "runtime" via [sourcetrack.Tracker.TrackValue]
//     but does NOT register a corresponding entry in c.fileTracker — the
//     fileTracker is the file-mtime/hash gate consumed by [Config.Reload]'s
//     incremental "did anything change?" filter, and runtime keys are not
//     file-backed so they have no mtime/hash to track. The two trackers
//     therefore diverge by design: sourceTracker reports runtime origin
//     for introspection (Explain / GetSourceInfo / GetConflicts), while
//     fileTracker only watches loader-backed files for incremental change
//     detection. The divergence is observable but harmless:
//
//   - When Reload's gate short-circuits because no underlying file
//     changed, runtime-Set keys persist in envConfig/mergedConfig and
//     OnChange does not fire spuriously.
//
//   - When Reload's gate fires because a file changed, Reload rebuilds
//     envConfig from the merged file data (Phase 4), so runtime-only
//     keys are evicted and OnChange correctly emits (runtimeValue, nil)
//     under the union-iteration deletion contract.
//
//     Both behaviors are documented Reload semantics: runtime mutations
//     are intentionally non-persistent across Reload because Reload commits
//     a fresh file-loaded view. Callers that need a runtime override to
//     survive Reload should re-Set after Reload, or use a [Config.Override]
//     restore handle. Adding a fileTracker.RegisterRuntimeKey API was
//     considered and rejected — runtime keys cannot be watched via
//     mtime/hash and the existing Wave 11 G12 source-tracker entry already
//     provides correct introspection.
func (c *Config[T]) Set(keyPath string, value any, opts ...SetOption) error {
	c.mu.Lock()
	// G13: see Reload for the rationale behind the manual unlock flag.
	// Failure paths fall through the deferred fallback; the success
	// path manually unlocks before invoking change callbacks so
	// callbacks may call back into the Config without deadlocking.
	unlocked := false
	defer func() {
		if !unlocked {
			c.mu.Unlock()
		}
	}()

	if c.frozen {
		return NewFrozenError("Set")
	}

	so := setOpts{allowOverride: true}
	for _, o := range opts {
		o(&so)
	}

	if !so.allowOverride && dictutil.HasNested(c.envConfig, keyPath) {
		return fmt.Errorf("key %q already exists (override=false)", keyPath)
	}

	// F-G10-SetInputAlias: defensively deep-copy any map/slice value so
	// later caller mutation does not alias into Config state. Scalars
	// pass through unchanged.
	stored := dictutil.DeepCopyValue(value)

	// F-Set-RollbackFidelity (Wave 17): structural snapshot/restore.
	// Pre-Wave 17 the rollback path used dictutil.Unflatten(Flatten(envConfig)),
	// which is lossy along two axes: (a) a yaml/json round-trip via the
	// flat-keys representation collapses non-string scalar types
	// (int -> float64, time.Duration -> int64) and drops nil-leaf
	// distinctions; (b) on a hypothetical future sub-tree Set the flat
	// representation cannot reconstruct the original sub-tree shape /
	// key ordering. Adopting the same Snapshot+Restore idiom Wave 7 G14
	// (Reload) and Wave 16 (Override) use guarantees structural fidelity
	// of envConfig, mergedConfig, and the source tracker on rollback.
	// Note: dictutil.SetNested may insert intermediate maps before
	// returning a *PathError, so the FIRST SetNested call must also
	// roll back envConfig (the pre-Wave 17 code skipped this path).
	envSnap := dictutil.DeepCopyValue(c.envConfig).(map[string]any)
	mergedSnap := dictutil.DeepCopyValue(c.mergedConfig).(map[string]any)
	trackerSnap := c.sourceTracker.Snapshot()

	// Snapshot pre-mutation flat state under the write lock (G13).
	// Used downstream for the change-notification payload.
	oldFlat := dictutil.Flatten(c.envConfig)

	// Try the mutation against envConfig first. SetNested returns a
	// *PathError when a key path traverses through a non-map; surface
	// that as a typed *ConfigError and do NOT mutate mergedConfig.
	if err := dictutil.SetNested(c.envConfig, keyPath, stored); err != nil {
		// Structural rollback (F-Set-RollbackFidelity): SetNested may have
		// inserted intermediate maps before failing, so restore envConfig
		// from the deep-copy snapshot. mergedConfig is untouched on this
		// path but we restore it (and the tracker) for symmetry with the
		// second-call failure path below.
		c.envConfig = envSnap
		c.mergedConfig = mergedSnap
		c.sourceTracker.Restore(trackerSnap)
		if c.observer != nil {
			c.observer.RecordSetFailed()
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("set_failed", keyPath, err)
		}
		return NewInvalidError("Set", keyPath, err)
	}
	// mergedConfig must agree with envConfig; the same path-error guard
	// applies. If this fails, roll back the envConfig mutation so the
	// two maps stay in lockstep.
	if err := dictutil.SetNested(c.mergedConfig, keyPath, stored); err != nil {
		// Structural rollback (F-Set-RollbackFidelity): restore envConfig
		// AND mergedConfig from their deep-copy snapshots to preserve
		// scalar type identity (int stays int, time.Duration stays
		// Duration), nil-leaf distinctions, and sub-tree shape. The
		// tracker snapshot is restored for symmetry with Wave 16
		// Override even though TrackValue has not yet fired on this path.
		c.envConfig = envSnap
		c.mergedConfig = mergedSnap
		c.sourceTracker.Restore(trackerSnap)
		if c.observer != nil {
			c.observer.RecordSetFailed()
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("set_failed", keyPath, err)
		}
		return NewInvalidError("Set", keyPath, err)
	}

	// Source-tracking parity: a successful Set claims "runtime" as the
	// source so Explain/GetSourceInfo report the runtime origin until a
	// subsequent Reload overwrites it.
	c.sourceTracker.TrackValue(keyPath, stored, "runtime", "runtime", c.env)

	c.validatedModel = nil

	// Snapshot everything callbacks/observability need WHILE the write
	// lock is held, then release the lock BEFORE invoking them (G13).
	callbacks := c.snapshotChangeCallbacks()
	newFlat := dictutil.Flatten(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)

	if observer != nil {
		observer.RecordSet()
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("set", keyPath, stored)
		emitter.Emit("change", keyPath, stored)
	}

	return nil
}

// Keys returns all dot-separated leaf key paths.
//
// When called with a prefix, only keys that start with that prefix are
// returned, and the keys retain their FULL dot-separated path (the prefix
// is NOT stripped). For example:
//
//	cfg.Set("db.host", "localhost")
//	cfg.Set("db.port", 5432)
//	cfg.Keys("db") // []string{"db.host", "db.port"}
//	for _, k := range cfg.Keys("db") {
//	    v, _ := cfg.Get(k) // each key works directly with Get
//	}
//
// G30 (Wave 21) — behavior tightening (no API signature change). Prior to
// Wave 21, Keys(prefix) stripped the prefix from each returned key (e.g.
// returned "host" / "port" for prefix "db"), which conflicted with the
// documented `for _, k := range cfg.Keys(p) { cfg.Get(k) }` iteration
// pattern: callers had to manually re-prepend the prefix before Get/Has
// would resolve. Keys now returns fully-qualified paths so callers can
// feed each key directly back into any access helper. Callers who need
// the suffix-only form can derive it with strings.TrimPrefix at the call
// site.
//
// The prefix accepts either "db" or "db." (the trailing dot is added if
// absent); both forms are equivalent.
func (c *Config[T]) Keys(prefix ...string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	if p == "" {
		keys := dictutil.FlatKeys(c.envConfig)
		sort.Strings(keys)
		return keys
	}
	// Normalize so callers can pass either "db" or "db." — both should
	// match keys "db.host", "db.port", etc.
	matchPrefix := p
	if !strings.HasSuffix(matchPrefix, ".") {
		matchPrefix += "."
	}
	all := dictutil.FlatKeys(c.envConfig)
	keys := make([]string, 0, len(all))
	for _, k := range all {
		if strings.HasPrefix(k, matchPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// ToDict returns the effective configuration as a hook-applied deep copy.
//
// ToDict is equivalent to [Config.ToDictCtx] with [context.Background].
// Errors raised by context-aware hooks are silently discarded — the
// pre-hook value is retained in their place. Callers that need to
// observe hook errors (for example, [secret.WithResolverFailOnMissing]
// fail-fast behavior) should use [Config.ToDictCtx].
//
// G11: prior to Wave 8 ToDict returned the live envConfig pointer
// without applying hooks, so callers saw raw `${secret:…}` placeholders
// and unexpanded `${ENV_VAR}` strings. The returned map is now both
// hook-applied and a defensive deep copy so callers can mutate it
// freely.
//
// G10 (Wave 12): the deep-copy contract is the read-side counterpart of
// the Wave 11 [Config.Set] write-side contract. Mutating the returned
// map cannot bleed into envConfig, cannot race against concurrent
// writers, and cannot be used to bypass [Config.Freeze]. The source is
// deep-copied while c.mu.RLock is held, then hooks run after the lock is
// released so user hook code may safely call back into Config APIs.
func (c *Config[T]) ToDict() map[string]any {
	d, _ := c.ToDictCtx(context.Background())
	return d
}

// ToDictCtx returns the effective configuration as a hook-applied deep
// copy, threading ctx through any registered context-aware hooks.
//
// Every leaf in the returned map is passed through the hook pipeline
// (env expansion, secret resolution, type casting, custom hooks) with
// its full dot-separated key path. The first error returned by a
// context-aware hook is propagated to the caller; subsequent leaves are
// not processed and the partially-resolved map is discarded. Legacy
// [hook.Func] hooks continue to run as before and cannot raise errors.
//
// The returned map is a deep copy of the live config: callers may
// mutate it without affecting the Config's state.
func (c *Config[T]) ToDictCtx(ctx context.Context) (map[string]any, error) {
	c.mu.RLock()
	src := c.envConfig
	if src == nil {
		src = c.mergedConfig
	}
	snapshot := dictutil.DeepCopy(src)
	c.mu.RUnlock()
	return c.applyHooksRecursive(ctx, "", snapshot)
}

// ---------------------------------------------------------------------------
// Introspection methods
// ---------------------------------------------------------------------------

// Explain returns detailed resolution information for a key.
//
// D08 (Wave 13): every value embedded in the returned map is defensively
// deep-copied via [dictutil.DeepCopyValue] so a caller mutating the
// result cannot bleed into envConfig or into the tracker's *SourceInfo.
// info.History is already a fresh slice (D06 GetSourceInfo defensive
// copy), but the per-entry Value field is shallow-copied at the
// SourceInfo layer; this method copies it again on the way out so the
// override_history payload is fully isolated.
func (c *Config[T]) Explain(keyPath string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info := c.sourceTracker.GetSourceInfo(keyPath)
	if info == nil {
		return map[string]any{
			"exists":         false,
			"key":            keyPath,
			"available_keys": dictutil.FlatKeys(c.envConfig),
		}
	}

	result := map[string]any{
		"exists":         true,
		"key":            keyPath,
		"value":          dictutil.DeepCopyValue(info.Value),
		"source":         info.SourceFile,
		"loader_type":    info.LoaderType,
		"environment":    c.env,
		"override_count": info.OverrideCount,
	}

	if len(info.History) > 0 {
		history := make([]map[string]any, 0, len(info.History))
		for _, h := range info.History {
			history = append(history, map[string]any{
				"value":       dictutil.DeepCopyValue(h.Value),
				"source":      h.Source,
				"loader_type": h.LoaderType,
			})
		}
		result["override_history"] = history
	}

	// Current value from live config — deep-copied so a caller mutating
	// the returned map cannot leak into envConfig (D08 / G10 parity).
	if val, ok := dictutil.GetNested(c.envConfig, keyPath); ok {
		result["current_value"] = dictutil.DeepCopyValue(val)
	}

	return result
}

// Schema returns schema information for a key.
//
// D08 (Wave 13): the embedded "value" is deep-copied via
// [dictutil.DeepCopyValue] so a caller mutating the returned map cannot
// alias envConfig (G10 parity on the introspection axis).
func (c *Config[T]) Schema(keyPath string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := map[string]any{"key": keyPath}

	val, ok := dictutil.GetNested(c.envConfig, keyPath)
	if !ok {
		result["exists"] = false
		return result
	}

	result["exists"] = true
	result["value"] = dictutil.DeepCopyValue(val)
	result["type"] = fmt.Sprintf("%T", val)

	return result
}

// Layers returns the layer stack showing each source and its keys.
//
// D08 (Wave 13): the per-layer map and its "keys" slice are freshly
// allocated on every call. Keys come from [sourcetrack.Tracker.FindKeysFromSource]
// which already returns a fresh []string. Mutations on any layer map
// returned here cannot bleed into tracker or envConfig state.
func (c *Config[T]) Layers() []map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := make(map[string]bool)
	var layers []map[string]any

	for _, l := range c.loaders {
		source := l.Source()
		if seen[source] {
			continue
		}
		seen[source] = true

		loaderType := loaderTypeName(l)

		keys := c.sourceTracker.FindKeysFromSource(source)
		// Defensive copy of the layer map: each successive call returns
		// a freshly-allocated map literal so the audit-flagged aliasing
		// hazard cannot reproduce. The "keys" slice is already a fresh
		// allocation from FindKeysFromSource; we copy the header into a
		// new []string so a caller appending or rewriting it cannot
		// reach across calls.
		keysCopy := append([]string(nil), keys...)
		layers = append(layers, map[string]any{
			"source":      source,
			"loader_type": loaderType,
			"keys":        keysCopy,
			"key_count":   len(keysCopy),
		})
	}
	return layers
}

// GetSourceInfo returns source tracking info for a key.
func (c *Config[T]) GetSourceInfo(keyPath string) *sourcetrack.SourceInfo {
	return c.sourceTracker.GetSourceInfo(keyPath)
}

// GetOverrideHistory returns the override history for a key.
func (c *Config[T]) GetOverrideHistory(keyPath string) []sourcetrack.OverrideEntry {
	return c.sourceTracker.GetOverrideHistory(keyPath)
}

// GetConflicts returns all keys that have been overridden.
func (c *Config[T]) GetConflicts() map[string]*sourcetrack.SourceInfo {
	return c.sourceTracker.GetConflicts()
}

// GetSourceStatistics returns aggregated source statistics.
func (c *Config[T]) GetSourceStatistics() map[string]any {
	return c.sourceTracker.GetSourceStatistics()
}

// FindKeysFromSource returns keys from sources matching the pattern.
func (c *Config[T]) FindKeysFromSource(pattern string) []string {
	return c.sourceTracker.FindKeysFromSource(pattern)
}

// PrintDebugInfo returns formatted debug info for a key (or all keys if empty).
func (c *Config[T]) PrintDebugInfo(keyPath string) string {
	return c.sourceTracker.PrintDebugInfo(keyPath)
}

// ExportDebugReport writes a full debug report as JSON.
func (c *Config[T]) ExportDebugReport(outputPath string) error {
	return c.sourceTracker.ExportDebugReport(outputPath)
}

// SourceTracker returns the source tracker for advanced inspection.
func (c *Config[T]) SourceTracker() *sourcetrack.Tracker {
	return c.sourceTracker
}

// ---------------------------------------------------------------------------
// Documentation generation
// ---------------------------------------------------------------------------

// GenerateDocs generates configuration documentation in the given format ("markdown" or "json").
func (c *Config[T]) GenerateDocs(format string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	flat := dictutil.Flatten(c.envConfig)
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type docEntry struct {
		Key          string `json:"key"`
		Type         string `json:"type"`
		CurrentValue any    `json:"current_value"`
		Source       string `json:"source"`
	}

	var entries []docEntry
	for _, k := range keys {
		v := flat[k]
		source := ""
		if info := c.sourceTracker.GetSourceInfo(k); info != nil {
			source = info.SourceFile
		}
		entries = append(entries, docEntry{
			Key: k, Type: fmt.Sprintf("%T", v), CurrentValue: v, Source: source,
		})
	}

	switch format {
	case "json":
		data, err := json.MarshalIndent(entries, "", "  ")
		return string(data), err

	case "markdown":
		var b strings.Builder
		b.WriteString("| Key | Type | Value | Source |\n")
		b.WriteString("|-----|------|-------|--------|\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "| `%s` | %s | `%v` | %s |\n", e.Key, e.Type, e.CurrentValue, e.Source)
		}
		return b.String(), nil

	default:
		return "", fmt.Errorf("unsupported docs format: %s (use \"markdown\" or \"json\")", format)
	}
}

// ---------------------------------------------------------------------------
// Lifecycle methods
// ---------------------------------------------------------------------------

// ReloadOption is a functional option that configures the behavior of
// [Config.Reload], such as enabling dry-run mode or overriding validation.
type ReloadOption func(*reloadOpts)
type reloadOpts struct {
	validate    *bool
	dryRun      bool
	incremental bool
}

// WithReloadValidate overrides validate_on_load for this reload. When true,
// the freshly loaded configuration is decoded into T and validated; on
// failure the entire reload is rolled back and a [ErrConfigValidation]
// is returned. When false, validation is skipped regardless of the
// Config's WithValidateOnLoad setting.
//
// G14: validation now runs whether the reload was driven by the
// incremental gate or by an unconditional WithIncremental(false), so a
// validation failure can never leave a "partially observed but not
// re-validated" state in place.
func WithReloadValidate(v bool) ReloadOption {
	return func(o *reloadOpts) { o.validate = &v }
}

// WithDryRun loads, validates, and then rolls back without applying
// changes. The Config's live state is unchanged on a successful dry-run.
//
// G14: dry-run rollback now runs *before* observability emission. A
// dry-run never produces a "reload" metric or event because the new
// state was not committed; if observability needs to record dry-run
// activity, register a listener on a future "dry_run" event (not yet
// emitted).
func WithDryRun(v bool) ReloadOption {
	return func(o *reloadOpts) { o.dryRun = v }
}

// WithIncremental enables the change-detection gate. When true (the
// default for [Config.Reload]), [Config.Reload] returns immediately if
// no tracked source has changed since the last load. When at least one
// source has changed, the full reload pipeline (load → validate →
// dry-run apply → commit-or-rollback) is executed.
//
// G14: the gate is purely a "should we reload at all?" filter — it does
// not implement true per-source incremental merge. Once the gate fires,
// all loaders run and validation/dry-run still apply; this differs from
// the pre-G14 behavior where the early-return path skipped validation
// entirely.
func WithIncremental(v bool) ReloadOption {
	return func(o *reloadOpts) { o.incremental = v }
}

// Reload reloads all configurations from their sources.
//
// The reload pipeline is structured as seven strictly-ordered phases
// (G14 — observability ordering contract):
//
//  1. Frozen-state check: refuses with [ErrConfigFrozen] if the Config
//     was frozen.
//  2. Incremental gate: when [WithIncremental] is true (the default),
//     returns immediately without invoking loaders if no tracked source
//     has changed. The fast-path emits no metrics or events.
//  3. Snapshot: the full live state (envConfig, mergedConfig, source
//     tracker) is snapshotted so a subsequent rollback restores
//     introspection alongside data (D05 / G14).
//  4. Load: c.load runs every loader and composer. On a Raise-policy
//     error the snapshots are restored, [Metrics.RecordReloadFailed] is
//     called, the "reload_failed" event is emitted, and the original
//     error is returned.
//  5. Validate: when validation is requested (either via Config-level
//     WithValidateOnLoad or per-call WithReloadValidate), the new state
//     is decoded into T and validated. On failure the snapshots are
//     restored and a [*ConfigError] wrapping [ErrConfigValidation] is
//     returned, also accompanied by RecordReloadFailed and a
//     "reload_failed" event.
//  6. Dry-run apply: when WithDryRun(true) is set, the snapshots are
//     restored as-if-validate-then-discard, no commit-time observability
//     fires, and Reload returns nil.
//  7. Commit: only after all earlier phases succeed do we emit the
//     "reload" metric/event, run change callbacks, and emit the
//     "change" event.
//
// Pre-G14, RecordReload and the "reload" event fired *after* c.load
// succeeded but *before* validation/dry-run rollback. That meant a
// validation-failure rollback left observability claiming the new state
// was applied; this is now fixed.
func (c *Config[T]) Reload(ctx context.Context, opts ...ReloadOption) error {
	c.mu.Lock()
	// G13: callbacks must run with c.mu released so a callback that
	// calls Get/Set/Reload/etc. cannot deadlock against the write
	// lock we hold here. We replace the simple `defer c.mu.Unlock()`
	// with a flag so the success path can release the lock manually
	// before invoking notifyChangesUnlocked, while every error/early
	// return continues to release through the deferred fallback.
	unlocked := false
	defer func() {
		if !unlocked {
			c.mu.Unlock()
		}
	}()

	if c.frozen {
		return NewFrozenError("Reload")
	}

	ro := reloadOpts{incremental: true}
	for _, o := range opts {
		o(&ro)
	}

	// Phase 2: Incremental gate. The gate is a pure "did anything
	// change?" filter — when nothing changed, we skip the entire
	// pipeline including loaders, validation, observability, and
	// callbacks. Returning nil here preserves the contract that an
	// unchanged reload is a true no-op (TestReload_Incremental_NoChange).
	if ro.incremental {
		paths := make([]string, 0, len(c.loaders))
		for _, l := range c.loaders {
			paths = append(paths, l.Source())
		}
		changed := c.fileTracker.GetChangedFiles(paths)
		if len(changed) == 0 {
			return nil
		}
	}

	// Phase 3: snapshot every component c.load mutates so the rollback
	// closure can restore them atomically. fileTracker participates
	// because c.load records mtime+sha256 of each loaded file; without
	// rolling it back, a subsequent incremental Reload would see the
	// recorded-bad hash and short-circuit the change-detection gate.
	oldEnv := copyMap(c.envConfig)
	oldMerged := copyMap(c.mergedConfig)
	trackerSnap := c.sourceTracker.Snapshot()
	fileTrackerSnap := c.fileTracker.Snapshot()
	start := time.Now()

	// rollback restores every snapshot taken in phase 3 and then drives
	// the failure-path observability hooks. It is a closure so that all
	// failure exits (load, validate) share the same restoration logic.
	rollback := func(failureErr error) {
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		c.fileTracker.Restore(fileTrackerSnap)
		// G14 ordering: failure-path metrics/events fire only after
		// rollback has restored the state, so any observer that probes
		// the Config from inside a "reload_failed" listener sees the
		// pre-reload values, not the doomed post-load ones.
		dur := time.Since(start)
		if c.observer != nil {
			c.observer.RecordReloadFailed(dur)
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("reload_failed", failureErr, dur)
		}
	}

	// Phase 4: Load.
	//
	// G08: reset the source tracker before c.load runs so a successful
	// reload starts from an empty tracker and re-populates from the new
	// layers. Pre-G08 the tracker was carried across reloads, which (a)
	// inflated override counts on every Reload of the same data because
	// each loader's TrackConfig retraced existing keys, and (b) left
	// stale entries for keys present in v1 but absent in v2 of a config.
	// Resetting via Restore on a zero-value Snapshot clears the live
	// tracker without touching trackerSnap, which the rollback closure
	// above still owns. Failure paths (load/validate) call rollback,
	// which Restores the pre-reload snapshot — preserving the Wave 7
	// G14 / D05 rollback contract.
	c.sourceTracker.Restore(sourcetrack.Snapshot{})
	if err := c.load(ctx); err != nil {
		rollback(err)
		return err
	}

	// Phase 5: Validate.
	//
	// G14: validation runs unconditionally on the incremental path too,
	// not only on full reloads. Pre-G14 the early-return gate at the top
	// of Reload could leave a Config in an unvalidated state when files
	// did change, because the gate path skipped validation. The gate
	// only short-circuits when nothing changed, in which case the
	// previously validated state is still current.
	shouldValidate := c.opts.ValidateOnLoad
	if ro.validate != nil {
		shouldValidate = *ro.validate
	}
	if shouldValidate {
		c.validatedModel = nil
		// G01: when a JSON Schema is configured (inline map or file
		// path), validate the new envConfig against it before the
		// struct decode below. Schema violations roll back via the
		// shared rollback closure to preserve the Wave 7 G14 / D05
		// snapshot contract. Sanitized public message; structured
		// detail on Context["schema_errors"].
		if c.jsonSchema != nil {
			msgs, serr := c.jsonSchema.ValidateDetailed(c.envConfig)
			if serr != nil {
				count := len(msgs)
				if count == 0 {
					count = 1
				}
				validationErr := &ConfigError{
					Op: "Reload",
					Err: fmt.Errorf(
						"%w: schema validation failed for %d constraint(s)",
						ErrConfigValidation, count,
					),
					Context: map[string]any{
						"schema_errors": msgs,
					},
				}
				rollback(validationErr)
				return validationErr
			}
		}
		if _, err := validate.DecodeAndValidate[T](c.envConfig); err != nil {
			validationErr := NewValidationError([]string{err.Error()}, err)
			rollback(validationErr)
			return validationErr
		}
	}

	// Phase 6: Dry run. Restore snapshots without firing the commit-path
	// observability — a dry-run that completes successfully is *not* a
	// reload from the perspective of metrics or events. The logger
	// records that the dry-run finished so operators have a trace.
	if ro.dryRun {
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		c.logger.Info("dry-run reload completed, changes not applied")
		return nil
	}

	// Phase 7: Commit. Only now is it safe to claim that a reload
	// happened — the new state is live and validated, callbacks below
	// can rely on it, and observability can record both reload and
	// change in the same critical section.
	c.validatedModel = nil
	duration := time.Since(start)
	if c.observer != nil {
		c.observer.RecordReload(duration)
	}
	if c.eventEmitter != nil {
		c.eventEmitter.Emit("reload", c.envConfig, duration)
	}

	// G13: snapshot everything callbacks need WHILE the write lock is
	// held, then release the lock BEFORE iterating callbacks. This
	// guarantees a callback that calls back into the Config (Get/Set/
	// Reload/Has/etc.) cannot deadlock on c.mu. The "newEnv" snapshot
	// is also used for the post-callback "change" event so that a
	// concurrent Set/Reload landing between unlock and Emit does not
	// lie about the payload that was observed.
	callbacks := c.snapshotChangeCallbacks()
	oldFlat := dictutil.Flatten(oldEnv)
	newFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)

	if observer != nil {
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("change", oldEnv, newEnv)
	}

	return nil
}

// Extend adds an additional loader at runtime and merges its config into
// the live state.
//
// Extend's lifecycle mirrors [Config.Reload]'s seven-phase pipeline so
// that runtime extension and reload share the same composition,
// environment resolution, file tracking, validation, snapshot/rollback,
// and observability semantics. Pre-G15, Extend was a partial merge path
// that bypassed all of those — calling Extend with a YAML file that
// contained a top-level "default:" map or "_include:" directive
// silently dropped the directive instead of resolving it. Post-G15:
//
//  1. Snapshot: live envConfig, mergedConfig, and source tracker are
//     captured so any failure restores them in lockstep (D05 / G14).
//  2. Load: l.Load(ctx) is invoked. Errors are dispatched through
//     c.opts.OnError (Raise → return, Warn → log+skip, Ignore →
//     silent-skip) exactly like the [Config.load] pipeline.
//  3. Compose: composition directives ("_include", "_defaults",
//     "_merge_strategy") in the loaded data are processed via
//     c.composer.Compose. Errors flow through the same OnError
//     dispatch as the loader phase.
//  4. Env-resolve: c.envHandler.Resolve folds env-keyed sections
//     (default + active env) so Extend honors the same flat-vs-env
//     contract that [Config.load] does. Pre-G15, an env-keyed file
//     passed to Extend would surface its raw "default:"/"production:"
//     branches directly into envConfig.
//  5. Merge: the resolved overlay is merged into both mergedConfig
//     and envConfig (envConfig because the overlay is already env-
//     resolved and should apply on top of the resolved state).
//  6. Validate: when c.opts.ValidateOnLoad is true and a schema is
//     configured, the new state is decoded into T and validated.
//     Failure rolls back every snapshot and returns a typed
//     [ErrConfigValidation].
//  7. Commit: only after every preceding phase succeeds do we append
//     to c.loaders, register the source with c.fileTracker (file-based
//     loaders only; non-file sources are silently skipped, pending the
//     G20 source-capability model), invalidate c.validatedModel,
//     update c.sourceTracker with the resolved overlay, emit the
//     "extend" metric and event, run change callbacks, and emit
//     "change". A "change" event paired with the extend overlay
//     payload allows operators to subscribe to runtime extensions
//     uniformly with reloads.
//
// On any failure path inside phases 2–6, c.observer.RecordExtendFailed
// is called and the "extend_failed" event is emitted; the original
// error is returned to the caller.
//
// G11 cache invalidation contract: c.validatedModel is set to nil only
// on commit. A failed Extend leaves the previous validated model
// reachable for [Config.Typed].
//
// G13 callback semantics: change callbacks are invoked AFTER c.mu has
// been released. A snapshot of the callback list, the pre-extend flat
// state, and the post-extend flat state is taken under the write lock
// in the commit phase; the lock is then dropped before
// notifyChangesUnlocked iterates the snapshots. This guarantees a
// callback that calls back into the Config (Get/Set/Reload/Extend/etc.)
// cannot deadlock against the write lock Extend held during the
// pipeline. Removed keys are reported uniformly with the Reload path
// (oldVal != nil, newVal == nil).
func (c *Config[T]) Extend(ctx context.Context, l Loader) error {
	c.mu.Lock()
	// G13: see Reload for the rationale behind the manual unlock flag.
	// Failure paths fall through the deferred fallback; the success
	// path manually unlocks before invoking change callbacks so
	// callbacks may call back into the Config without deadlocking.
	unlocked := false
	defer func() {
		if !unlocked {
			c.mu.Unlock()
		}
	}()

	if c.frozen {
		return NewFrozenError("Extend")
	}

	// Phase 1: Snapshot live state for rollback. Mirrors Reload phase 3.
	oldEnv := copyMap(c.envConfig)
	oldMerged := copyMap(c.mergedConfig)
	trackerSnap := c.sourceTracker.Snapshot()
	start := time.Now()

	// rollback restores every snapshot taken in phase 1 and drives the
	// failure-path observability hooks. It is a closure so every
	// failure exit (load, compose, validate) shares the same restoration
	// logic, matching the Reload rollback pattern.
	rollback := func(failureErr error) {
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		dur := time.Since(start)
		if c.observer != nil {
			c.observer.RecordExtendFailed(dur)
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("extend_failed", failureErr, dur)
		}
	}

	// Phase 2: Load.
	data, err := l.Load(ctx)
	if err != nil {
		switch c.opts.OnError {
		case ErrorPolicyRaise:
			rollback(err)
			return err
		case ErrorPolicyWarn:
			c.logger.Warn(
				"loader error",
				slog.String("source", l.Source()),
				slog.String("error", err.Error()),
			)
			// Warn skips the loader: nothing to commit, but no failure
			// either. Snapshots are not restored because nothing was
			// mutated; we simply return without commit-time observability.
			return nil
		case ErrorPolicyIgnore:
			// Silent: distinct from Warn, do not emit a log record.
			return nil
		default:
			// Unknown policy values are treated as Raise so that
			// misconfiguration surfaces loudly, mirroring c.load.
			rollback(err)
			return err
		}
	}
	if data == nil {
		// Graceful absence: the loader had nothing to contribute. No
		// snapshot rollback needed because nothing was mutated, and no
		// commit-time observability fires.
		return nil
	}

	// Phase 3: Compose. Process _include / _defaults / _merge_strategy
	// directives. Errors flow through OnError, mirroring c.load.
	composed, err := c.composer.Compose(data, l.Source())
	if err != nil {
		switch c.opts.OnError {
		case ErrorPolicyRaise:
			rollback(err)
			return err
		case ErrorPolicyWarn:
			c.logger.Warn(
				"composition error",
				slog.String("source", l.Source()),
				slog.String("error", err.Error()),
			)
			composed = data
		case ErrorPolicyIgnore:
			composed = data
		default:
			rollback(err)
			return err
		}
	}

	// Phase 4: Env-resolve. The composer output may carry env-keyed
	// sections (default / <env>); honor them through the same handler
	// c.load uses so Extend and load agree on the env-handling contract.
	resolved := c.envHandler.Resolve(composed, c.env)

	// Phase 5: Merge into live state. Compute the new envConfig /
	// mergedConfig as candidates so a later validation failure can be
	// rolled back via the snapshot taken in phase 1.
	c.mergedConfig = c.merger.Merge(c.mergedConfig, composed)
	c.envConfig = c.merger.Merge(c.envConfig, resolved)

	// Phase 6: Validate. When validate-on-load is configured AND a
	// schema is present, decode and validate the new state. Pre-G15
	// Extend skipped this entirely, so an extension that violated the
	// validator was silently committed.
	//
	// G01: a JSON Schema (inline map or compiled file path) is now
	// honored in addition to the struct path. Schema violations roll
	// back via the shared closure with a sanitized public message and
	// the structured violation list on Context["schema_errors"].
	if c.opts.ValidateOnLoad {
		if c.jsonSchema != nil {
			msgs, serr := c.jsonSchema.ValidateDetailed(c.envConfig)
			if serr != nil {
				count := len(msgs)
				if count == 0 {
					count = 1
				}
				validationErr := &ConfigError{
					Op: "Extend",
					Err: fmt.Errorf(
						"%w: schema validation failed for %d constraint(s)",
						ErrConfigValidation, count,
					),
					Context: map[string]any{
						"schema_errors": msgs,
					},
				}
				rollback(validationErr)
				return validationErr
			}
		}
		if c.opts.Schema != nil {
			if _, isMap := c.opts.Schema.(map[string]any); !isMap {
				if _, verr := validate.DecodeAndValidate[T](c.envConfig); verr != nil {
					validationErr := NewValidationError([]string{verr.Error()}, verr)
					rollback(validationErr)
					return validationErr
				}
			}
		}
	}

	// Phase 7: Commit. Only now do we mutate non-rollback-protected
	// state (loaders slice, file tracker, source tracker for the new
	// loader, validated-model cache invalidation) and emit commit-time
	// observability.
	c.loaders = append(c.loaders, l)

	// Track the source with the file tracker so subsequent Reload calls
	// can detect changes to it. Non-file sources (HTTP, env, etc.)
	// produce a Stat error which we silently ignore — the G20 source-
	// capability model will replace this best-effort approach.
	if ferr := c.fileTracker.Track(l.Source()); ferr != nil {
		c.logger.Debug(
			"extend: source not tracked for incremental reload",
			slog.String("source", l.Source()),
			slog.String("reason", ferr.Error()),
		)
	}

	// Track the resolved overlay against this loader so Explain /
	// Layers / GetSourceInfo report this source for every key it
	// contributed (matching c.load's per-loader TrackConfig pattern).
	loaderType := loaderTypeName(l)
	c.sourceTracker.TrackConfig(resolved, l.Source(), loaderType, c.env, "")

	c.validatedModel = nil
	duration := time.Since(start)
	if c.observer != nil {
		c.observer.RecordExtend(duration)
	}
	if c.eventEmitter != nil {
		c.eventEmitter.Emit("extend", c.envConfig, duration)
	}

	// G13: snapshot callbacks + flat state under the write lock, then
	// release the lock before iterating callbacks. Mirrors the Reload
	// commit phase so the deadlock and deletion-miss fix applies
	// symmetrically to runtime extension.
	callbacks := c.snapshotChangeCallbacks()
	oldFlat := dictutil.Flatten(oldEnv)
	newFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)

	if observer != nil {
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("change", oldEnv, newEnv)
	}

	return nil
}

// Override temporarily overrides configuration values.
// Returns a restore function that must be called (typically via defer) to revert.
//
// G13 (F-G13-Override): Override fires registered OnChange callbacks
// for every key whose value differs between the pre-override and
// post-override flat state. Callbacks observe the deletion contract
// uniformly with [Config.Reload] and [Config.Extend]: a key whose
// value is replaced surfaces (oldVal, newVal); a key that did not
// exist before but is introduced by the override surfaces
// (nil, newVal). Callbacks fire AFTER c.mu has been released so a
// callback that calls back into the Config cannot deadlock.
//
// The restore function returned from Override likewise fires
// OnChange callbacks for every key that the restoration mutates,
// so observers can react symmetrically to override / restore cycles.
// Restore-time callbacks run with c.mu released for the same
// deadlock-safety reason.
//
// Override is LIFO-composable: each call pushes a frame onto an
// internal stack, and the returned restore removes its own frame
// regardless of stack position. While the stack is non-empty, live
// envConfig / mergedConfig / source tracker are derived by replaying
// remaining frames onto the captured base; a fully-drained stack
// returns the Config to its pre-Override state. The closure is
// idempotent — a second call is a no-op.
//
// Out-of-order restore (popping a non-top frame) is supported. The
// rebuild calls TrackValue on each surviving frame, which inflates
// OverrideCount on those keys; the alternative — per-frame inverse-
// delta bookkeeping — was rejected as overhead for an already-rare
// path.
//
// An unrestored frame keeps c.frozen = false (Override clears it to
// permit nested overrides). Callers that have not relinquished an
// override scope cannot Freeze the Config.
func (c *Config[T]) Override(overrides map[string]any) (restore func(), err error) {
	c.mu.Lock()

	wasEmpty := len(c.overrideStack) == 0

	// Capture the override base on the empty → non-empty transition.
	// The base is consumed only when the stack drains; nested pushes
	// just append.
	if wasEmpty {
		c.overrideBaseEnv = dictutil.DeepCopy(c.envConfig)
		c.overrideBaseMerged = dictutil.DeepCopy(c.mergedConfig)
		c.overrideBaseTracker = c.sourceTracker.Snapshot()
		c.overrideBaseFrozen = c.frozen
	}

	preOverrideEnv := copyMap(c.envConfig)
	preOverrideMerged := copyMap(c.mergedConfig)
	preOverrideTrackerSnap := c.sourceTracker.Snapshot()
	preOverrideFrozen := c.frozen
	c.frozen = false

	// A SetNested *PathError on any key halts the override, restores
	// the pre-Override snapshots (not the override base — surviving
	// frames below this push must remain applied), and surfaces the
	// error as a typed *ConfigError.
	frame := &overrideFrame{
		id:      c.overrideIDCounter + 1,
		payload: make(map[string]any, len(overrides)),
		applied: true,
	}
	c.overrideIDCounter = frame.id
	for k, v := range overrides {
		// Defensive deep copy: a caller mutating the overrides map
		// after Override returns must not bleed into Config state.
		stored := dictutil.DeepCopyValue(v)
		frame.payload[k] = stored
		if serr := dictutil.SetNested(c.envConfig, k, stored); serr != nil {
			c.envConfig = preOverrideEnv
			c.mergedConfig = preOverrideMerged
			c.frozen = preOverrideFrozen
			c.sourceTracker.Restore(preOverrideTrackerSnap)
			if wasEmpty {
				c.overrideBaseEnv = nil
				c.overrideBaseMerged = nil
				c.overrideBaseTracker = sourcetrack.Snapshot{}
			}
			observer := c.observer
			emitter := c.eventEmitter
			c.mu.Unlock()
			if observer != nil {
				observer.RecordOverrideFailed()
			}
			if emitter != nil {
				emitter.Emit("override_failed", k, serr)
			}
			return nil, NewInvalidError("Override", k, serr)
		}
		if serr := dictutil.SetNested(c.mergedConfig, k, stored); serr != nil {
			c.envConfig = preOverrideEnv
			c.mergedConfig = preOverrideMerged
			c.frozen = preOverrideFrozen
			c.sourceTracker.Restore(preOverrideTrackerSnap)
			if wasEmpty {
				c.overrideBaseEnv = nil
				c.overrideBaseMerged = nil
				c.overrideBaseTracker = sourcetrack.Snapshot{}
			}
			observer := c.observer
			emitter := c.eventEmitter
			c.mu.Unlock()
			if observer != nil {
				observer.RecordOverrideFailed()
			}
			if emitter != nil {
				emitter.Emit("override_failed", k, serr)
			}
			return nil, NewInvalidError("Override", k, serr)
		}
		// Source label "override" (vs Set's "runtime") so Explain can
		// distinguish runtime mutations from override-scope mutations.
		c.sourceTracker.TrackValue(k, stored, "override", "override", c.env)
	}
	c.overrideStack = append(c.overrideStack, frame)
	c.validatedModel = nil

	// Snapshot the callback list and pre/post flat state under the
	// write lock; iterate callbacks after release so a callback that
	// re-enters the Config cannot deadlock on c.mu.
	overrideCallbacks := c.snapshotChangeCallbacks()
	overrideOldFlat := dictutil.Flatten(preOverrideEnv)
	overrideNewFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()

	c.notifyChangesUnlocked(overrideCallbacks, overrideOldFlat, overrideNewFlat)

	if observer != nil {
		observer.RecordOverride()
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("override", overrides)
		emitter.Emit("change", preOverrideEnv, newEnv)
	}

	restore = c.makeOverrideRestore(frame)
	return restore, nil
}

// makeOverrideRestore returns the restore closure for frame. The
// closure removes frame from c.overrideStack regardless of position
// and rebuilds live state from the override base plus surviving
// frames in original push order. The applied flag is mutated only
// under c.mu so concurrent restores on peer frames cannot race; the
// idempotent fast path is the !applied early return.
func (c *Config[T]) makeOverrideRestore(frame *overrideFrame) func() {
	return func() {
		c.mu.Lock()
		if !frame.applied {
			c.mu.Unlock()
			return
		}
		frame.applied = false

		preRestoreEnv := copyMap(c.envConfig)

		// Remove frame from any position in the stack.
		newStack := make([]*overrideFrame, 0, len(c.overrideStack))
		for _, f := range c.overrideStack {
			if f.id == frame.id {
				continue
			}
			newStack = append(newStack, f)
		}
		c.overrideStack = newStack

		if len(c.overrideStack) == 0 {
			// Stack drained: restore base atomically. frozen returns
			// to whatever it was when the first override pushed.
			c.envConfig = c.overrideBaseEnv
			c.mergedConfig = c.overrideBaseMerged
			c.sourceTracker.Restore(c.overrideBaseTracker)
			c.frozen = c.overrideBaseFrozen
			c.overrideBaseEnv = nil
			c.overrideBaseMerged = nil
			c.overrideBaseTracker = sourcetrack.Snapshot{}
		} else {
			// Surviving frames: rebuild from a fresh deep copy of the
			// base (so we do not mutate the persisted snapshot) and
			// replay each frame's payload in push order.
			c.envConfig = dictutil.DeepCopy(c.overrideBaseEnv)
			c.mergedConfig = dictutil.DeepCopy(c.overrideBaseMerged)
			c.sourceTracker.Restore(c.overrideBaseTracker)
			for _, f := range c.overrideStack {
				for k, v := range f.payload {
					// Replay-time SetNested failure means a Set or
					// Reload during override scope reshaped the tree;
					// log and skip. The scope is best-effort under
					// concurrent mutation.
					if serr := dictutil.SetNested(c.envConfig, k, v); serr != nil {
						if c.logger != nil {
							c.logger.Warn(
								"override replay skipped key",
								slog.String("key", k),
								slog.Uint64("frame_id", f.id),
								slog.String("error", serr.Error()),
							)
						}
						continue
					}
					if serr := dictutil.SetNested(c.mergedConfig, k, v); serr != nil {
						if c.logger != nil {
							c.logger.Warn(
								"override replay skipped key on mergedConfig",
								slog.String("key", k),
								slog.Uint64("frame_id", f.id),
								slog.String("error", serr.Error()),
							)
						}
						continue
					}
					c.sourceTracker.TrackValue(k, v, "override", "override", c.env)
				}
			}
			c.frozen = false
		}
		c.validatedModel = nil

		restoreCallbacks := c.snapshotChangeCallbacks()
		restoreOldFlat := dictutil.Flatten(preRestoreEnv)
		restoreNewFlat := dictutil.Flatten(c.envConfig)
		newEnv := copyMap(c.envConfig)
		restoreObserver := c.observer
		restoreEmitter := c.eventEmitter

		c.mu.Unlock()

		c.notifyChangesUnlocked(restoreCallbacks, restoreOldFlat, restoreNewFlat)

		if restoreObserver != nil {
			restoreObserver.RecordOverrideRestored()
			restoreObserver.RecordChange()
		}
		if restoreEmitter != nil {
			restoreEmitter.Emit("override_restored", newEnv)
			restoreEmitter.Emit("change", preRestoreEnv, newEnv)
		}
	}
}

// Export serializes the config to the given format ("json", "yaml", "toml").
// If outputPath is provided, also writes to that file.
//
// Export is equivalent to [Config.ExportCtx] with [context.Background].
// Hook errors raised by context-aware hooks are propagated to the
// caller and no file write is attempted.
//
// G11: prior to Wave 8 Export marshaled the raw `c.envConfig` without
// applying hooks, leaking unresolved `${secret:…}` placeholders into
// exported artifacts. Export now applies the hook pipeline to a deep
// copy of the config before serializing, so exported JSON/YAML/TOML
// reflects the same effective values seen by [Config.Get] and
// [Config.Typed].
//
// G10 (Wave 12): Export takes its deep-copy snapshot while c.mu.RLock
// is held and releases the lock once the snapshot is owned privately.
// Hooks and marshaling then run against the private snapshot rather
// than against live envConfig, which closes the historical
// "unlocks-before-marshaling" race that let a concurrent Set tear the
// JSON/YAML output. The lock is released BEFORE hooks and marshaling so
// slow or re-entrant user code cannot block or deadlock writers.
func (c *Config[T]) Export(format string, outputPath ...string) ([]byte, error) {
	return c.ExportCtx(context.Background(), format, outputPath...)
}

// ExportCtx serializes the config to the given format ("json", "yaml",
// "toml"), threading ctx through any registered context-aware hooks.
// If outputPath is provided, also writes to that file.
//
// Hook errors raised by context-aware hooks are propagated to the
// caller and no serialization or file write is attempted.
func (c *Config[T]) ExportCtx(ctx context.Context, format string, outputPath ...string) ([]byte, error) {
	data, err := c.ToDictCtx(ctx)
	if err != nil {
		return nil, err
	}

	var result []byte

	switch format {
	case "json":
		result, err = json.MarshalIndent(data, "", "  ")
	case "yaml":
		result, err = yaml.Marshal(data)
	case "toml":
		var buf strings.Builder
		enc := toml.NewEncoder(&buf)
		err = enc.Encode(data)
		result = []byte(buf.String())
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
	if err != nil {
		return nil, err
	}

	if len(outputPath) > 0 && outputPath[0] != "" {
		if err := os.WriteFile(outputPath[0], result, 0644); err != nil {
			return result, err
		}
	}

	return result, nil
}

// Freeze makes the config immutable.
func (c *Config[T]) Freeze() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frozen = true
}

// IsFrozen returns whether the config is frozen.
func (c *Config[T]) IsFrozen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.frozen
}

// Env returns the active environment name.
func (c *Config[T]) Env() string { return c.env }

// OnChange registers a callback that fires whenever configuration
// values change.
//
// Firing surfaces. The callback is invoked from each of the following
// mutation entry points, once per key whose flattened value differs
// between the pre- and post-mutation snapshots:
//   - [Config.Set]                        (G12 / Wave 11)
//   - [Config.Reload]
//   - [Config.Extend]
//   - [Config.Override]
//   - the restore closure returned by [Config.Override]
//
// Lock-release contract (G13). Callbacks run AFTER the Config's internal
// write lock has been released. A callback may therefore freely call
// back into any public method on the Config — Get, Set, Has, Reload,
// Extend, Override — without deadlocking. The trade-off is that the
// Config state visible to a callback is whatever any concurrent
// goroutine has produced by the time the callback runs; the (oldVal,
// newVal) payload, however, is taken from a snapshot captured at the
// moment of the change and is stable regardless of subsequent mutations.
//
// Self-firing recursion (Wave 11 G12 / Wave 18). Because Set, Override,
// Reload, Extend, and the Override restore closure all themselves fire
// OnChange callbacks, a callback that mutates the Config will TRIGGER
// another OnChange invocation for that nested mutation. This is a
// load-bearing feature — it is what lets a callback transparently
// notify downstream listeners — but it means callers are responsible
// for guarding against unbounded recursion. The canonical idiom is an
// [atomic.Bool] CAS one-shot, NOT [sync.Once]: a recursive Once.Do
// deadlocks (see the documented Once contract), whereas atomic.Bool
// CAS lets the first entry into the callback win the right to mutate
// and subsequent self-fires become cheap no-ops. See
// TestOnChange_GodocContract_Recursion in config_callbacks_test.go for
// a runnable example. A typical guard reads:
//
//	var fired atomic.Bool
//	cfg.OnChange(func(key string, oldVal, newVal any) {
//	    if fired.CompareAndSwap(false, true) {
//	        _ = cfg.Set("derived.key", deriveFrom(newVal))
//	    }
//	})
//
// Diff-based firing (G13). The callback fires for every key whose
// pre/post flattened value differs. The (oldVal, newVal) payload uses
// untyped `any`:
//   - replaced key:   fn(key, oldVal, newVal) — both non-nil.
//   - removed key:    fn(key, oldVal, nil)    — newVal is the zero value.
//   - introduced key: fn(key, nil, newVal)    — oldVal is the zero value.
//
// Callbacks that need to distinguish "explicitly set to nil" from
// "removed" must compare against a sentinel themselves; the contract
// only guarantees that removal surfaces the zero `any`.
//
// Panic recovery (G13). A panicking callback does NOT abort sibling
// callbacks, nor does it abort the calling goroutine: each callback
// runs inside an independent recover() guard. A panic is logged via
// the Config's slog logger and the next callback in the registration
// order continues normally.
func (c *Config[T]) OnChange(fn func(key string, oldVal, newVal any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.changeCallbacks = append(c.changeCallbacks, fn)
}

// HookProcessor returns the hook processor for registering custom hooks.
func (c *Config[T]) HookProcessor() *hook.Processor { return c.hookProcessor }

// Logger returns the [*slog.Logger] resolved at construction time.
//
// The returned pointer is the same value that internal subsystems
// (loaders, hooks, validators) write through, making it the canonical
// observable for [WithLogger] and the self-config `log_level` setting
// (G04). Mutations to the logger reference itself (rebinding) are not
// supported through this accessor; supply a different logger via
// [WithLogger] at construction time instead.
func (c *Config[T]) Logger() *slog.Logger { return c.logger }

// Diff compares this config with another config.
func (c *Config[T]) Diff(other *Config[T]) []diff.ConfigDiff {
	return diff.Diff(c.ToDict(), other.ToDict())
}

// DetectDrift compares this config against an intended baseline.
func (c *Config[T]) DetectDrift(intended map[string]any) []diff.ConfigDiff {
	return diff.Diff(intended, c.ToDict())
}

// ---------------------------------------------------------------------------
// Observability integration
// ---------------------------------------------------------------------------

// EnableObservability enables access/reload/change metrics collection.
func (c *Config[T]) EnableObservability() *observe.Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observer == nil {
		c.observer = observe.NewMetrics(len(dictutil.FlatKeys(c.envConfig)))
	}
	return c.observer
}

// EnableEvents enables event emission.
func (c *Config[T]) EnableEvents() *observe.EventEmitter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eventEmitter == nil {
		c.eventEmitter = observe.NewEventEmitter(c.logger)
	}
	return c.eventEmitter
}

// EnableVersioning configures snapshot persistence under storagePath
// with a maxVersions ring cap. The configuration always takes effect:
// if a [observe.VersionManager] already exists — typically because
// [Config.SaveVersion] lazy-created one — it is reconfigured in place
// via [observe.VersionManager.Reconfigure], preserving the in-memory
// snapshot ring. Subsequent SaveVersion calls write to storagePath.
//
// EnableVersioning is safe to call multiple times; the latest call's
// arguments become authoritative.
func (c *Config[T]) EnableVersioning(storagePath string, maxVersions int) *observe.VersionManager {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager(storagePath, maxVersions)
	} else {
		c.versionMgr.Reconfigure(storagePath, maxVersions)
	}
	return c.versionMgr
}

// GetMetrics returns current observability metrics. Returns nil if not enabled.
func (c *Config[T]) GetMetrics() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.observer == nil {
		return nil
	}
	return c.observer.Statistics()
}

// SaveVersion captures the current config as an immutable version
// snapshot. If no [observe.VersionManager] exists yet, one is
// lazy-created with in-memory defaults; a later [Config.EnableVersioning]
// call reconfigures the same manager in place. The lazy allocation
// runs under c.mu so concurrent first callers cannot double-initialize.
func (c *Config[T]) SaveVersion(metadata map[string]any) (*observe.Version, error) {
	c.mu.Lock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager("", 0)
	}
	mgr := c.versionMgr
	envSnapshot := dictutil.DeepCopy(c.envConfig)
	c.mu.Unlock()
	return mgr.SaveVersion(envSnapshot, metadata)
}

// RollbackToVersion restores the config to a previous version snapshot.
func (c *Config[T]) RollbackToVersion(versionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.frozen {
		return NewFrozenError("RollbackToVersion")
	}
	if c.versionMgr == nil {
		return fmt.Errorf("versioning not enabled")
	}

	v := c.versionMgr.GetVersion(versionID)
	if v == nil {
		return fmt.Errorf("version %s not found", versionID)
	}

	// Deep-copy the snapshot before assigning into live state. Aliasing
	// v.Config directly let later mutations of mergedConfig corrupt the
	// stored snapshot, so a subsequent rollback to the same version would
	// observe drifted (post-mutation) state instead of the captured one.
	snapshot := dictutil.DeepCopy(v.Config)
	c.envConfig = snapshot
	c.mergedConfig = dictutil.DeepCopy(snapshot)
	c.validatedModel = nil
	return nil
}

// StopWatching stops the file watcher if running.
func (c *Config[T]) StopWatching() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watcher != nil {
		c.watcher.Stop()
		c.watcher = nil
	}
}

func (c *Config[T]) startWatching() {
	// G20-residual (Wave 21): filter non-file loader sources at the call
	// site before handing them to the watch package. Wave 20 Fixer-AT
	// added a defensive scheme allowlist inside watch.New that emits a
	// "skipping non-file source" warning on every reload — useful as a
	// safety net, but noisy when the call site can trivially pre-filter.
	// We duplicate the small scheme-prefix check here (D-W20-01 tracks
	// extracting it into a shared internal package next wave) so loaders
	// like envPrefixAutoLoader ("environment:APP"), HTTPLoader
	// ("http(s)://..."), and cloud-store loaders ("s3://", "ssm:",
	// "gs://", "azure://", "ibmcos://", "git:", "consul://", "vault:")
	// never reach watch.New on the happy path.
	var files []string
	for _, l := range c.loaders {
		src := l.Source()
		if isNonFileLoaderSource(src) {
			continue
		}
		files = append(files, src)
	}
	if len(files) == 0 {
		// All loader sources are non-file (e.g. env-only configs).
		// Skip watcher construction entirely so we don't hold an
		// fsnotify handle for nothing.
		return
	}
	w, err := watch.New(files, func() error {
		return c.Reload(context.Background())
	}, c.logger)
	if err != nil {
		c.logger.Warn("failed to start file watcher", slog.String("error", err.Error()))
		return
	}
	c.watcher = w
}

// isNonFileLoaderSource reports whether a Loader.Source() identifier is one
// of the URL-style or marker-prefix forms whose backing storage cannot be
// observed via fsnotify.
//
// D-W20-01 (Wave 22): the canonical allowlist is now owned by
// [github.com/confiify/confii-go/internal/sourcekind]. This wrapper preserves
// the local call-site name; the three formerly-divergent lists (watch,
// sourcetrack, confii) all share the consolidated predicate.
func isNonFileLoaderSource(s string) bool {
	return sourcekind.IsNonFileSource(s)
}

// ---------------------------------------------------------------------------
// Typed access
// ---------------------------------------------------------------------------

// Typed decodes the configuration into a typed struct T and validates it.
//
// Typed is equivalent to [Config.TypedCtx] with [context.Background].
// Hook errors raised by context-aware hooks (for example a
// [secret.Resolver] configured with [secret.WithResolverFailOnMissing])
// are propagated as a [*ConfigError] wrapping [ErrConfigValidation].
//
// G11: prior to Wave 8 Typed decoded raw `c.envConfig` straight into T
// without applying hooks, so a `${secret:db/password}` placeholder ended
// up as the literal string in the typed struct while [Config.Get]
// returned the resolved secret. Typed now applies the hook pipeline to
// a deep copy of the source map before decoding so the typed model and
// scalar Get agree on the effective configuration.
func (c *Config[T]) Typed() (*T, error) {
	return c.TypedCtx(context.Background())
}

// TypedCtx decodes the configuration into a typed struct T and validates
// it, applying the hook pipeline to every leaf before decoding.
//
// The supplied ctx is threaded through every registered context-aware
// hook. The first error returned by a hook (or by validation) is
// propagated; the validated-model cache is only populated when the
// caller passes a context-free invocation (i.e. via [Config.Typed]).
// Callers passing a request-scoped ctx always re-run hooks so that
// per-request context (deadlines, cancellation, propagated values)
// affects the materialized model.
func (c *Config[T]) TypedCtx(ctx context.Context) (*T, error) {
	// Cache fast-path: only valid for the canonical context-free call,
	// which is signalled by ctx == context.Background(). A caller-supplied
	// ctx may carry per-request values that the resolver depends on, so
	// re-run the pipeline rather than returning a possibly-stale model.
	cacheable := ctx == context.Background()

	c.mu.RLock()
	if cacheable && c.validatedModel != nil {
		model := c.validatedModel
		c.mu.RUnlock()
		return model, nil
	}
	snapshot := dictutil.DeepCopy(c.envConfig)
	c.mu.RUnlock()

	// Hooks are user code and may perform I/O or re-enter Config. Run them
	// against the private snapshot without holding c.mu. This also keeps a
	// slow resolver from blocking unrelated readers and writers.
	resolved, err := c.applyHooksRecursive(ctx, "", snapshot)
	if err != nil {
		return nil, NewValidationError([]string{err.Error()}, err)
	}

	model, err := validate.DecodeAndValidate[T](resolved)
	if err != nil {
		return nil, NewValidationError([]string{err.Error()}, err)
	}
	if !cacheable {
		return model, nil
	}

	// Publish the context-free cache only if live state still matches the
	// snapshot that was resolved. A hook may have re-entered Set/Reload while
	// we were unlocked; caching that now-stale model would make future Typed
	// calls return data older than envConfig. If another caller populated a
	// valid cache first, prefer it.
	c.mu.Lock()
	if c.validatedModel != nil {
		model = c.validatedModel
	} else if reflect.DeepEqual(snapshot, c.envConfig) {
		c.validatedModel = model
	}
	c.mu.Unlock()
	return model, nil
}

// String returns a human-readable summary of the Config, including its
// environment, key count, loader sources, and frozen state.
func (c *Config[T]) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sources := make([]string, 0, len(c.loaders))
	for _, l := range c.loaders {
		sources = append(sources, l.Source())
	}
	frozen := ""
	if c.frozen {
		frozen = ", frozen"
	}
	return fmt.Sprintf("Config(env=%q, keys=%d, sources=%v%s)",
		c.env, len(dictutil.FlatKeys(c.envConfig)), sources, frozen)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// applyHooksRecursive walks a configuration map and returns a deep copy
// with the hook pipeline (env expansion, secret resolution, type
// casting, and any registered custom hooks) applied to every leaf.
//
// Each leaf is processed via [hook.Processor.ProcessCtx] with its
// absolute dot-separated key path so per-key hooks see the same path
// they would for a scalar [Config.GetCtx] call. Slice elements are
// recursed into; their key path uses the parent key (no index suffix)
// to match the hook contract — every hook should be index-agnostic
// today, but this is documented here so a future indexed-key hook
// extension is a deliberate change.
//
// The returned map is a fresh allocation and shares no mutable state
// with the input, satisfying the hook-applied "defensive copy"
// contract that all access modes (Get whole-map, Typed, ToDict,
// Export) now rely on for G11.
//
// applyHooksRecursive is read-only with respect to Config state and holds
// no Config locks. Callers pass a private snapshot so hooks can safely call
// back into Config APIs without deadlocking. The hook processor manages its
// own concurrency.
func (c *Config[T]) applyHooksRecursive(ctx context.Context, prefix string, m map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		keyPath := k
		if prefix != "" {
			keyPath = prefix + "." + k
		}
		switch tv := v.(type) {
		case map[string]any:
			sub, err := c.applyHooksRecursive(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[k] = sub
		case []any:
			arr, err := c.applyHooksToSlice(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[k] = arr
		default:
			processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, v)
			if err != nil {
				return nil, err
			}
			out[k] = processed
		}
	}
	return out, nil
}

// applyHooksToSlice mirrors applyHooksRecursive for []any slices, which
// commonly appear in YAML/JSON configurations (e.g. lists of hosts,
// arrays of feature-flag overrides). Each element is hook-applied at
// the parent key path; nested maps and slices are recursed into.
func (c *Config[T]) applyHooksToSlice(ctx context.Context, keyPath string, items []any) ([]any, error) {
	out := make([]any, len(items))
	for i, item := range items {
		switch tv := item.(type) {
		case map[string]any:
			sub, err := c.applyHooksRecursive(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[i] = sub
		case []any:
			arr, err := c.applyHooksToSlice(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[i] = arr
		default:
			processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, item)
			if err != nil {
				return nil, err
			}
			out[i] = processed
		}
	}
	return out, nil
}

// snapshotChangeCallbacks returns a defensive copy of the registered
// change callbacks. Callers MUST hold c.mu (read or write) when invoking
// this helper. The returned slice is owned by the caller and may be
// iterated outside the lock without racing against concurrent OnChange
// registrations.
func (c *Config[T]) snapshotChangeCallbacks() []func(key string, oldVal, newVal any) {
	if len(c.changeCallbacks) == 0 {
		return nil
	}
	out := make([]func(key string, oldVal, newVal any), len(c.changeCallbacks))
	copy(out, c.changeCallbacks)
	return out
}

// notifyChangesUnlocked fires the supplied callbacks for every key that
// differs between oldFlat and newFlat. It MUST be called with c.mu
// released so that callbacks may freely call back into the public API
// (Get/Set/Reload/etc.) without deadlocking on c.mu.
//
// G13 deletion semantics: this iterates the UNION of key sets, so:
//   - keys present in both with different values fire (oldVal, newVal).
//   - keys present in old only fire (oldVal, nil) — the deletion case.
//   - keys present in new only fire (nil, newVal) — the addition case.
//   - keys present in both with equal values are skipped.
//
// Callback panics are recovered per-callback so a single misbehaving
// listener cannot prevent peers from observing the change.
func (c *Config[T]) notifyChangesUnlocked(
	callbacks []func(key string, oldVal, newVal any),
	oldFlat, newFlat map[string]any,
) {
	if len(callbacks) == 0 {
		return
	}
	// Iterate the union of keys so removed keys (present in oldFlat,
	// absent from newFlat) and newly introduced keys (present in
	// newFlat, absent from oldFlat) are both reported.
	seen := make(map[string]struct{}, len(oldFlat)+len(newFlat))
	for key := range oldFlat {
		seen[key] = struct{}{}
	}
	for key := range newFlat {
		seen[key] = struct{}{}
	}
	for key := range seen {
		oldVal, hadOld := oldFlat[key]
		newVal, hasNew := newFlat[key]
		// Suppress only when both sides are present AND structurally
		// equal under reflect.DeepEqual.
		//
		// reflect.DeepEqual is type-aware: int(8080) and float64(8080)
		// are not equal, so a JSON-decoded float64 replacing an int
		// correctly fires the callback for downstream re-coercion.
		if hadOld && hasNew && reflect.DeepEqual(oldVal, newVal) {
			continue
		}
		// Removed keys surface (oldVal, nil); added keys surface
		// (nil, newVal) — the deletion contract.
		var emitOld, emitNew any
		if hadOld {
			emitOld = oldVal
		}
		if hasNew {
			emitNew = newVal
		}
		for i, cb := range callbacks {
			c.invokeChangeCallback(cb, i, key, emitOld, emitNew)
		}
	}
}

// invokeChangeCallback runs cb under a recover guard. A panicking
// callback is logged at error level on c.logger with the affected
// key, the callback index, the recovered value, and a runtime stack
// trace. Sibling callbacks continue regardless.
func (c *Config[T]) invokeChangeCallback(
	cb func(key string, oldVal, newVal any),
	callbackIndex int,
	key string,
	oldVal, newVal any,
) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if c.logger != nil {
			c.logger.Error(
				"OnChange callback panic recovered",
				slog.String("key", key),
				slog.Int("callback_index", callbackIndex),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())),
			)
		}
	}()
	cb(key, oldVal, newVal)
}

func (c *Config[T]) lookupSysenv(keyPath string) (any, bool) {
	envName := strings.ToUpper(strings.ReplaceAll(keyPath, ".", "_"))
	if c.opts.EnvPrefix != "" {
		envName = strings.ToUpper(c.opts.EnvPrefix) + "_" + envName
	}
	val, ok := os.LookupEnv(envName)
	if !ok {
		return nil, false
	}
	return val, true
}

// loaderTypeName returns the underlying type name of a Loader implementation,
// suitable for use in source tracking and debug output.
//
// The Loader interface accepts both pointer and value receiver implementations,
// so we cannot assume reflect.TypeOf(l) returns a pointer kind. Calling
// reflect.Type.Elem on a non-pointer (and non-array/chan/map/slice) type panics
// with "reflect: Elem of invalid type", which previously broke any custom
// Loader registered as a value type. reflect.Indirect on the value normalizes
// pointer and value loaders to the same underlying type before we read the
// name. If the type is anonymous (Name() == ""), we fall back to the
// reflect.Type string representation so the loader still surfaces something
// meaningful in debug output.
func loaderTypeName(l Loader) string {
	t := reflect.TypeOf(l)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if name := t.Name(); name != "" {
		return name
	}
	return reflect.TypeOf(l).String()
}

func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			result[k] = copyMap(sub)
		} else {
			result[k] = v
		}
	}
	return result
}

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
		// Drive through WithEnvPrefix so the canonical
		// envPrefixAutoLoader is appended (G03) AND the explicitlySet
		// flag is stamped — preventing a later self-config layer from
		// double-applying the prefix.
		WithEnvPrefix(settings.DefaultPrefix)(opts)
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

	// `sources` appends a declarative loader chain. Each entry is a
	// {type, path|prefix, ...} map. Honors the `loaders` explicitlySet
	// flag with the same "skip when explicit" semantics as default_files,
	// so explicit code (WithLoaders / Builder.AddLoader) wins.
	if !opts.isSet("loaders") && len(settings.Sources) > 0 {
		for _, src := range settings.Sources {
			if err := appendSelfConfigSource(opts, src); err != nil {
				return err
			}
		}
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

// appendSelfConfigSource translates one self-config `sources:` entry
// into a [Loader] and appends it to opts.Loaders. Recognised types
// match the file-format dispatch already implemented by
// [fileAutoLoader] (yaml, yml, json, toml, ini, cfg, env, envfile)
// plus "environment" / "env-vars" for an OS-environment loader keyed
// by `prefix`. Unknown types surface as typed *ConfigError so operator
// typos in `.confii.yaml` fail loudly instead of silently dropping a
// declared source.
func appendSelfConfigSource(opts *options, src map[string]any) error {
	rawType, _ := src["type"].(string)
	t := strings.ToLower(strings.TrimSpace(rawType))
	switch t {
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
		return &ConfigError{
			Op: "ApplySelfConfig",
			Err: fmt.Errorf(
				"%w: unsupported self-config source type %q (supported: yaml, yml, json, toml, ini, cfg, env, envfile, environment)",
				ErrConfigLoad, rawType,
			),
		}
	}
}

// resolveSchemaValidator compiles a JSON Schema validator from the option
// state, if one is configured. It is the single source of truth for how
// [WithSchema] and [WithSchemaPath] are interpreted by the validate-on-load
// pipeline (G01). The resolution order is:
//
//  1. opts.Schema, when its concrete type is map[string]any: compile via
//     [validate.NewJSONSchemaValidator].
//  2. opts.SchemaPath, when non-empty AND opts.Schema is not already a
//     map[string]any: read the file and compile via
//     [validate.NewJSONSchemaValidatorFromFile]. SchemaPath is honored
//     only when no inline JSON Schema is provided so that the typical
//     "either-or" caller intent is preserved without ambiguity.
//  3. Anything else (struct value, nil, primitive sentinel): return
//     (nil, nil). Struct-shaped schemas drive [Config.Typed]'s
//     mapstructure decode + validator.v10 path; the absence of a JSON
//     Schema validator is the documented signal that struct validation
//     applies instead.
//
// Compile failures (malformed schema map, missing/invalid file) are
// surfaced as a typed [*ConfigError] wrapping [ErrConfigValidation] so
// callers can detect them with [errors.Is] / [errors.As].
func resolveSchemaValidator(opts *options) (*validate.JSONSchemaValidator, error) {
	if m, ok := opts.Schema.(map[string]any); ok {
		v, err := validate.NewJSONSchemaValidator(m)
		if err != nil {
			return nil, &ConfigError{
				Op:  "New",
				Err: fmt.Errorf("%w: compile inline JSON schema: %v", ErrConfigValidation, err),
			}
		}
		return v, nil
	}
	if opts.SchemaPath != "" {
		v, err := validate.NewJSONSchemaValidatorFromFile(opts.SchemaPath)
		if err != nil {
			return nil, &ConfigError{
				Op:     "New",
				Source: opts.SchemaPath,
				Err:    fmt.Errorf("%w: load JSON schema from path: %v", ErrConfigValidation, err),
			}
		}
		return v, nil
	}
	return nil, nil
}

// runValidateOnLoad executes the validate-on-load pipeline for a freshly
// loaded envConfig. It is called from both [New] (via the Step 6 hook
// site) and from the [Config.Reload] / [Config.Extend] validate phases so
// that the three lifecycles agree on which validator runs and how its
// errors are surfaced (G01).
//
// Two validators may run, in this order:
//
//  1. JSON Schema validator (when c.jsonSchema is non-nil): the resolved
//     envConfig is validated against the compiled schema. Failures
//     return a typed [*ConfigError] wrapping [ErrConfigValidation] with
//     a sanitized public message ("schema validation failed for N
//     constraint(s)") and the full structured violation list on
//     Context["schema_errors"]. The raw values of violating keys are
//     never embedded in the public error message — programmatic callers
//     read Context.
//  2. Struct validator (when opts.Schema is a non-nil non-map value):
//     [validate.DecodeAndValidate] runs the existing struct-tag
//     validation. Failures are wrapped via [NewValidationError] so the
//     ErrConfigValidation sentinel chain is preserved.
//
// When both validators are configured (a JSON Schema map + a struct
// type, an unusual combination), JSON Schema runs first because it
// gates structural correctness before mapstructure decode, which can
// otherwise mask schema-level violations behind type-cast errors.
//
// Honors [WithStrictValidation]: when strict is false, the legacy
// behavior (warn-and-continue on Typed-style validation) is preserved
// for the struct path. JSON Schema violations always return an error
// when validate-on-load is true regardless of strict mode, because the
// schema is a hard contract — a non-strict downgrade would be the same
// silent-stub behavior G01 was filed to remove.
func (c *Config[T]) runValidateOnLoad() error {
	if !c.opts.ValidateOnLoad {
		return nil
	}

	// JSON Schema path (G01): hard fail on violation.
	if c.jsonSchema != nil {
		msgs, err := c.jsonSchema.ValidateDetailed(c.envConfig)
		if err != nil {
			count := len(msgs)
			if count == 0 {
				count = 1
			}
			// Sanitized public message: count of violations, no raw
			// values. Full structured detail is on Context.
			return &ConfigError{
				Op: "Validate",
				Err: fmt.Errorf(
					"%w: schema validation failed for %d constraint(s)",
					ErrConfigValidation, count,
				),
				Context: map[string]any{
					"schema_errors": msgs,
				},
			}
		}
	}

	// Struct path: only when Schema is a struct-shaped value (i.e. not
	// a JSON Schema map and not nil). For nil Schema with no SchemaPath
	// we are a no-op — backward compatible with the documented
	// "WithValidateOnLoad without schema is no-op" contract.
	if c.opts.Schema != nil {
		if _, isMap := c.opts.Schema.(map[string]any); !isMap {
			if _, err := c.Typed(); err != nil {
				if c.opts.StrictValidation {
					return err
				}
				c.logger.Warn(
					"validation failed on load",
					slog.String("error", err.Error()),
				)
			}
		}
	}

	return nil
}

// fileAutoLoader is the loader used by the self-config `default_files`
// auto-discovery path. It auto-detects the file format from the file
// extension and dispatches to the same parsing logic as the user-facing
// loaders in the loader subpackage.
//
// Supported file extensions (D07 / G19-residual):
//
//   - .yaml, .yml: YAML — keys are recursively normalized via
//     [dictutil.NormalizeKeys] so non-string-keyed YAML (e.g. integer or
//     boolean keys) never leaks map[interface{}]interface{} into caller-
//     visible state. This is the Wave 9 D01 contract; before D07 closure
//     this code path bypassed the normalization, allowing the leak.
//   - .json: JSON via [encoding/json].
//   - .toml: TOML via [github.com/BurntSushi/toml].
//   - .ini, .cfg: INI via [gopkg.in/ini.v1]; defaults-only sections (the
//     synthetic DEFAULT section preceding the first explicit [section]
//     header) are promoted to root keys to match [loader.INILoader]'s
//     G19 contract.
//   - .env: KEY=VALUE pairs with comment + quote support, mirroring the
//     [loader.EnvFileLoader] format.
//
// Any other extension produces a typed *ConfigError wrapping
// [ErrConfigFormat] — silently falling back to YAML (the pre-D07
// behavior for unknown extensions) is no longer permitted, since it
// masked operator typos like `config.xml` or `config.cong`.
//
// Missing files are dispatched through the loader's [ErrorPolicy]:
// under [ErrorPolicyRaise] (default for explicit-loader parity) a typed
// *ConfigError wrapping [ErrConfigLoad] is returned; under
// [ErrorPolicyWarn] the absence is logged and (nil, nil) is returned;
// under [ErrorPolicyIgnore] the file is silently skipped. The policy
// is inherited from the Config-level [WithOnError] when applySelfConfig
// constructs the loader, providing parity with the explicit-loader
// per-loader errorPolicy contract (G07).
type fileAutoLoader struct {
	path        string
	errorPolicy ErrorPolicy
	logger      *slog.Logger
}

// Source returns the file path identifying this loader's source.
func (l *fileAutoLoader) Source() string { return l.path }

// Load reads, parses, and normalizes the configured file. See
// [fileAutoLoader] for the supported-extension list, the D01
// normalization contract for YAML, and the missing-file policy
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

	format := formatparse.FromExtension(l.path)
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
		// D07: unknown extension is now a typed format error, not a
		// silent YAML fallback. Operator typos (config.xml, config.cong)
		// surface visibly instead of producing a misleading YAML parse
		// error or — worse — silently parsing arbitrary content as YAML.
		return nil, NewFormatError(l.path, string(format),
			fmt.Errorf("unsupported file format for self-config default_files entry %q (supported: .yaml, .yml, .json, .toml, .ini, .cfg, .env)", l.path))
	}
}

func (l *fileAutoLoader) loadYAML() (map[string]any, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, NewLoadError(l.path, err)
	}
	// D01: decode into an untyped value so we can normalize maps with
	// non-string keys via dictutil.NormalizeKeys (the same helper that
	// loader.YAMLLoader.Load uses). Pre-D07 this path decoded directly
	// into map[string]any, which gopkg.in/yaml.v3 emits as
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
	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		return nil, NewFormatError(l.path, "toml", err)
	}
	return result, nil
}

func (l *fileAutoLoader) loadINI() (map[string]any, error) {
	cfg, err := ini.Load(l.path)
	if err != nil {
		return nil, NewFormatError(l.path, "ini", err)
	}
	result := make(map[string]any)
	for _, section := range cfg.Sections() {
		name := section.Name()
		// Mirror loader.INILoader's G19 contract: the synthetic
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
	f, err := os.Open(l.path)
	if err != nil {
		return nil, NewLoadError(l.path, err)
	}
	defer func() { _ = f.Close() }()

	result := make(map[string]any)
	scanner := bufio.NewScanner(f)
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
// missing-file contract (G19-residual / D07).
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
