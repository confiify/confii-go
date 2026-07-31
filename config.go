// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/confiify/confii-go/v2/compose"
	"github.com/confiify/confii-go/v2/envhandler"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/merge"
	"github.com/confiify/confii-go/v2/observe"
	"github.com/confiify/confii-go/v2/sourcetrack"
	"github.com/confiify/confii-go/v2/validate"
	"github.com/confiify/confii-go/v2/watch"
)

// Config owns one fully materialized configuration lifecycle. It loads ordered
// sources, selects an environment, applies transformations and secret
// resolution, validates the result, and atomically publishes the snapshot read
// by [Config.Get], [Config.ToDict], and [Config.Typed]. The type parameter T is
// the model decoded by Typed; use any when only dynamic access is required.
//
// All exported methods are safe for concurrent use. Read methods observe a
// complete published snapshot, while mutation methods build and validate a
// replacement before publication. Call [Config.Close] when dynamic reloading
// or another managed background resource has been enabled.
type Config[T any] struct {
	mu sync.RWMutex
	// revision changes on every successful live-state mutation. Long-running
	// transactional operations compare it before publishing a private candidate.
	revision uint64

	// Core state.
	// unresolvedEnvConfig is the selected, merged environment before the
	// hook pipeline runs. It retains secret references for refresh and
	// value-safe provider introspection. envConfig is the eagerly materialized
	// application snapshot served by every read API.
	unresolvedEnvConfig map[string]any
	envConfig           map[string]any
	mergedConfig        map[string]any
	// sensitivePaths records paths derived from secret references independently
	// of the resolved snapshot so diagnostics remain safe after materialization.
	sensitivePaths map[string]struct{}
	frozen         bool
	env            string

	// Collaborators.
	loaders []Loader
	// loaderLayers stores each loader's composed contribution in precedence
	// order. Reload uses it to rebuild the merged configuration while loading
	// only sources selected by the incremental change detector.
	loaderLayers []map[string]any
	// loaderDependencies contains the transitive _include files read while
	// composing each layer, aligned by loader index.
	loaderDependencies [][]string
	merger             Merger
	hookProcessor      *hook.Processor
	envHandler         *envhandler.Handler
	sourceTracker      *sourcetrack.Tracker
	fileTracker        *sourcetrack.FileTracker
	composer           *compose.Composer
	exporters          map[string]Exporter

	// Observability (nil until enabled).
	observer         *observe.Metrics
	metricsReader    MetricsReader
	eventEmitter     *observe.EventEmitter
	eventSubscriber  EventSubscriber
	versionMgr       *observe.VersionManager
	versionReader    VersionReader
	watcher          *watch.Watcher
	watchCancel      context.CancelFunc
	closeOnce        sync.Once
	closed           bool
	resourceRegistry *selfConfigResourceCollector

	// Settings.
	opts   options
	logger *slog.Logger

	// Typed model cache.
	validatedModel *T

	// sourcePlan is rebuilt atomically with the resolved configuration and
	// exposed through SourcePlan for preflight/runtime introspection.
	sourcePlan SourcePlan

	// Compiled JSON Schema validator.
	//
	// Populated lazily by [resolveSchemaValidator] from either a
	// map[string]any schema passed via [WithSchema] or a file path
	// passed via [WithSchemaPath]. nil when the caller supplied a
	// struct-shaped schema (struct validation flows through Typed()
	// instead) or no schema at all. Reused across [Config.New] and
	// [Config.Reload] so the schema file is read only once.
	jsonSchema *validate.JSONSchemaValidator

	// Change callbacks.
	changeCallbacks        []func(key string, oldVal, newVal any)
	changeContextCallbacks []func(context.Context, string, any, any)

	// Override stack. Each call to [Config.Override] pushes a frame;
	// the returned restore closure removes its own frame regardless of
	// stack position. When the stack is non-empty the live envConfig /
	// mergedConfig / sourceTracker are derived by replaying remaining
	// frames onto the captured base. The base is captured when the
	// stack transitions empty → non-empty, so a fully-drained stack
	// returns the Config to its pre-Override state atomically. See
	// [Config.Override] for the LIFO composability contract.
	overrideStack         []*overrideFrame
	overrideIDCounter     uint64
	overrideBaseEnv       map[string]any
	overrideBaseRawEnv    map[string]any
	overrideBaseMerged    map[string]any
	overrideBaseTracker   sourcetrack.Snapshot
	overrideBaseSensitive map[string]struct{}
	overrideBaseFrozen    bool
}

// Logger returns the logger resolved when the Config was constructed. The
// returned pointer is also used by Confii's loaders, hooks, and validators.
// Supply a different logger through [WithLogger] before construction rather
// than attempting to replace it through this accessor.
func (c *Config[T]) Logger() *slog.Logger { return c.logger }

// New constructs a ready-to-read Config using an implicit background context.
// The startup timeout configured by [WithStartupTimeout] bounds source loading,
// provider authentication, transformation, secret resolution, and validation.
// No Config is returned unless the complete initial snapshot succeeds.
//
// Use [NewWithContext] when initialization must inherit caller cancellation,
// context values, or an existing deadline.
//
// A minimal typed construction is:
//
//	type AppConfig struct {
//		Port int `confii:"port" validate:"required,min=1,max=65535"`
//	}
//	cfg, err := confii.New[AppConfig](
//		confii.WithLoaders(loader.NewYAML("config.yaml")),
//		confii.WithValidateOnLoad(true),
//	)
//	if err != nil { return err }
//	defer cfg.Close()
func New[T any](cfgOpts ...Option) (*Config[T], error) {
	return NewWithContext[T](context.Background(), cfgOpts...)
}

// NewWithContext constructs a ready-to-read Config and propagates ctx through
// source loading, provider setup, hooks, secret resolution, and validation.
// A nil context is rejected. If ctx has no deadline, Confii applies the
// fallback configured by [WithStartupTimeout]; an existing deadline is never
// replaced or extended.
//
// Effective settings follow this precedence: constructor options, project
// self-configuration, then built-in defaults. Loaders are evaluated in order
// and later layers take precedence according to the configured merge policy.
//
// The call returns a typed [*ConfigError] for invalid options, self-config,
// source failures, composition failures, unresolved required secrets, and
// validation failures. Any resources created before an error are closed; no
// partially initialized Config is returned.
//
// [WithFreezeOnLoad](true) and [WithDynamicReloading](true) are mutually
// exclusive because a frozen Config cannot accept watcher-driven reloads.
func NewWithContext[T any](ctx context.Context, cfgOpts ...Option) (*Config[T], error) {
	if ctx == nil {
		return nil, &ConfigError{Op: "New", Code: ConfigErrorCodeLoad, Err: errors.New("nil context")}
	}
	opts := defaultOptions()
	for index, fn := range cfgOpts {
		if fn == nil {
			return nil, NewInvalidError("New", "", fmt.Errorf("option %d is nil", index))
		}
		if err := applyConstructionOption(fn, &opts, index); err != nil {
			return nil, err
		}
	}

	// Apply project self-configuration before validating effective options.
	//
	// Absence is a supported state, represented by selfconfig.Read returning
	// nil settings and no error. A discovered but unreadable or malformed
	// self-config is unsafe to ignore: it may change source precedence,
	// validation, or secret handling, so fail closed with a typed error.
	if err := applySelfConfig(&opts); err != nil {
		var ce *ConfigError
		if errors.As(err, &ce) {
			return nil, err
		}
		return nil, &ConfigError{Op: "New", Code: ConfigErrorCodeLoad, Err: fmt.Errorf("read self-config: %w", err)}
	}
	if err := validateAndOwnOptions(&opts); err != nil {
		return nil, err
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && opts.StartupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.StartupTimeout)
		defer cancel()
	}
	resourceCollector := newSelfConfigResourceCollector()
	ctx = context.WithValue(ctx, selfConfigResourceCollectorContextKey{}, resourceCollector)
	constructed := false
	defer func() {
		if constructed {
			return
		}
		for _, resource := range resourceCollector.closeAndSnapshot() {
			if closer, ok := resource.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
		}
	}()

	// Resolve the active environment before building environment-dependent
	// sources and secret providers.
	opts.Env = envhandler.ResolveEnv(opts.Env, opts.isSet("env"), opts.EnvSwitcher, opts.Env)
	if err := resolveEnvironmentStrategy(&opts); err != nil {
		return nil, err
	}
	if !opts.isSet("secret_hook") && opts.SecretHook == nil && len(opts.selfConfigSecrets) > 0 {
		h, defaultProvider, providers, err := buildSelfConfigSecretHookForEnvironment(ctx, opts.selfConfigSecrets, opts.Env)
		if err != nil {
			return nil, err
		}
		opts.SecretHook = h
		opts.selfConfigSecretProvider = defaultProvider
		opts.selfConfigSecretProviders = providers
	}

	// Declarative sources preserve their configured order and are materialized
	// after environment selection.
	for _, src := range opts.selfConfigSources {
		if err := appendSelfConfigSource(ctx, &opts, src); err != nil {
			return nil, err
		}
	}
	ensureEnvPrefixLoader(&opts)

	// One AdvancedMerger applies both the default strategy and per-path rules.
	var m Merger = merge.NewAdvanced(opts.MergeStrategy, opts.MergeStrategyMap)

	// Validate extension collaborators before schema or source I/O.
	exporters, err := buildExporterRegistry(opts.Exporters)
	if err != nil {
		return nil, err
	}
	if err := validateCustomValidators(opts.Validators); err != nil {
		return nil, err
	}
	// Compile the configured JSON Schema before loading sources. The compiled
	// validator is reused by reload and extension operations.
	jsonSchema, err := resolveSchemaValidator(&opts)
	if err != nil {
		return nil, err
	}

	// Resolve relative composition paths from the configured working
	// directory. An empty working directory uses the process directory.
	composerBase := opts.WorkingDir
	if composerBase == "" {
		composerBase = "."
	}

	c := &Config[T]{
		env:              opts.Env,
		loaders:          opts.Loaders,
		merger:           m,
		hookProcessor:    hook.NewProcessor(),
		envHandler:       envhandler.New(opts.Logger),
		sourceTracker:    sourcetrack.NewTracker(opts.DebugMode),
		fileTracker:      sourcetrack.NewFileTracker(),
		composer:         compose.New(composerBase, compose.WithMerger(m)),
		exporters:        exporters,
		opts:             opts,
		logger:           opts.Logger,
		jsonSchema:       jsonSchema,
		resourceRegistry: resourceCollector,
	}

	// Register transformation hooks in materialization order.
	if opts.UseEnvExpander {
		c.hookProcessor.RegisterGlobalHook(hook.NewEnvExpanderHook())
	}
	if opts.UseTypeCasting {
		c.hookProcessor.RegisterGlobalHook(hook.NewTypeCastHook())
	}
	for _, setup := range opts.hookSetups {
		switch setup.kind {
		case hookSetupKey:
			c.hookProcessor.RegisterKeyHook(setup.key, setup.hook)
		case hookSetupValue:
			c.hookProcessor.RegisterValueHook(setup.value, setup.hook)
		case hookSetupCondition:
			c.hookProcessor.RegisterConditionHook(setup.condition, setup.hook)
		case hookSetupGlobal:
			c.hookProcessor.RegisterGlobalHook(setup.hook)
		}
	}
	// Secret resolution is the final transformation step. Custom hooks may
	// normalize or synthesize a reference, but no hook observes resolved secret
	// material unless the application explicitly performs that work itself.
	if opts.SecretHook != nil {
		c.hookProcessor.RegisterGlobalHook(opts.SecretHook)
	}

	// Load and merge all configured sources.
	if err := c.load(ctx); err != nil {
		return nil, err
	}

	// Materialize the effective configuration before publication. The unresolved
	// selected snapshot is retained for explicit secret refresh and reference
	// introspection.
	if err := c.materializeEffectiveConfig(ctx); err != nil {
		return nil, &ConfigError{
			Op:   "New",
			Code: ConfigErrorCodeLoad,
			Err:  fmt.Errorf("materialize effective configuration: %w", err),
		}
	}
	// Validate the materialized snapshot before publication.
	if err := c.runValidateOnLoad(); err != nil {
		return nil, err
	}

	// Apply the requested runtime lifecycle state.
	if opts.FreezeOnLoad {
		c.frozen = true
	}

	if opts.DynamicReloading {
		if err := c.startWatching(); err != nil {
			return nil, &ConfigError{Op: "New", Code: ConfigErrorCodeLoad, Err: fmt.Errorf("start dynamic reloading: %w", err)}
		}
	}

	constructed = true
	return c, nil
}
