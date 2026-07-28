// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"github.com/confiify/confii-go/compose"
	"github.com/confiify/confii-go/envhandler"
	"github.com/confiify/confii-go/hook"
	"github.com/confiify/confii-go/merge"
	"github.com/confiify/confii-go/observe"
	"github.com/confiify/confii-go/sourcetrack"
	"github.com/confiify/confii-go/validate"
	"github.com/confiify/confii-go/watch"
	"log/slog"
	"os"
	"sync"
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

	// sourcePlan is rebuilt atomically with the resolved configuration and
	// exposed through SourcePlan for preflight/runtime introspection.
	sourcePlan SourcePlan

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
	// Absence is a supported state, represented by selfconfig.Read returning
	// nil settings and no error. A discovered but unreadable or malformed
	// self-config is unsafe to ignore: it may change source precedence,
	// validation, or secret handling, so fail closed with a typed error.
	if err := applySelfConfig(&opts); err != nil {
		var ce *ConfigError
		if errors.As(err, &ce) {
			return nil, err
		}
		return nil, &ConfigError{Op: "New", Err: fmt.Errorf("%w: read self-config: %w", ErrConfigLoad, err)}
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
	if err := resolveEnvironmentStrategy(&opts); err != nil {
		return nil, err
	}
	if !opts.isSet("secret_hook") && opts.SecretHook == nil && len(opts.selfConfigSecrets) > 0 {
		h, defaultProvider, providers, err := buildSelfConfigSecretHookForEnvironment(opts.selfConfigSecrets, opts.Env)
		if err != nil {
			return nil, err
		}
		opts.SecretHook = h
		opts.selfConfigSecretProvider = defaultProvider
		opts.selfConfigSecretProviders = providers
	}

	// Declarative sources are materialized only after environment selection.
	// This is observable only for the opt-in `environment_files` source;
	// existing source types retain their previous order and behavior.
	for _, src := range opts.selfConfigSources {
		if err := appendSelfConfigSource(ctx, &opts, src); err != nil {
			return nil, err
		}
	}
	ensureEnvPrefixLoader(&opts)

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
