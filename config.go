package confii

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
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
	"github.com/confiify/confii-go/merge"
	"github.com/confiify/confii-go/observe"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/confiify/confii-go/sourcetrack"
	"github.com/confiify/confii-go/validate"
	"github.com/confiify/confii-go/watch"
	"gopkg.in/yaml.v3"
)

// Config is the main configuration manager, parameterized by T for typed access.
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

	// Change callbacks.
	changeCallbacks []func(key string, oldVal, newVal any)
}

// New creates a new Config instance, loading and merging all sources.
//
// Initialization follows the priority: explicit argument > self-config file > built-in default.
func New[T any](ctx context.Context, cfgOpts ...Option) (*Config[T], error) {
	opts := defaultOptions()
	for _, fn := range cfgOpts {
		fn(&opts)
	}

	// Step 1: Read self-configuration.
	if err := applySelfConfig(&opts); err != nil {
		opts.Logger.Warn("failed to read self-config", slog.String("error", err.Error()))
	}

	// Step 2: Resolve environment.
	if opts.EnvSwitcher != "" {
		if envVal := os.Getenv(opts.EnvSwitcher); envVal != "" {
			opts.Env = envVal
		}
	}

	// Step 3: Set up merger.
	var m Merger = merge.NewDefault(opts.DeepMerge)
	if opts.MergeStrategy != nil {
		m = merge.NewAdvanced(*opts.MergeStrategy, opts.MergeStrategyMap)
	}

	c := &Config[T]{
		env:           opts.Env,
		loaders:       opts.Loaders,
		merger:        m,
		hookProcessor: hook.NewProcessor(),
		envHandler:    envhandler.New(opts.Logger),
		sourceTracker: sourcetrack.NewTracker(opts.DebugMode),
		fileTracker:   sourcetrack.NewFileTracker(),
		composer:      compose.New("."),
		opts:          opts,
		logger:        opts.Logger,
	}

	// Step 4: Register default hooks in order: env expander, then type casting.
	if opts.UseEnvExpander {
		c.hookProcessor.RegisterGlobalHook(hook.NewEnvExpanderHook())
	}
	if opts.UseTypeCasting {
		c.hookProcessor.RegisterGlobalHook(hook.NewTypeCastHook())
	}

	// Step 5: Load all configurations.
	if err := c.load(ctx); err != nil {
		return nil, err
	}

	// Step 6: Validate on load.
	if opts.ValidateOnLoad && opts.Schema != nil {
		if _, err := c.Typed(); err != nil {
			if opts.StrictValidation {
				return nil, err
			}
			c.logger.Warn("validation failed on load", slog.String("error", err.Error()))
		}
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
func (c *Config[T]) load(ctx context.Context) error {
	var configs []map[string]any

	for _, l := range c.loaders {
		data, err := l.Load(ctx)
		if err != nil {
			if c.opts.OnError == ErrorPolicyRaise {
				return err
			}
			c.logger.Warn("loader error", slog.String("source", l.Source()), slog.String("error", err.Error()))
			continue
		}
		if data == nil {
			continue
		}

		// Process composition directives (_include, _defaults).
		composed, err := c.composer.Compose(data, l.Source())
		if err != nil {
			c.logger.Warn("composition error", slog.String("source", l.Source()), slog.String("error", err.Error()))
			composed = data
		}

		// Track source.
		loaderType := reflect.TypeOf(l).Elem().Name()
		if loaderType == "" {
			loaderType = reflect.TypeOf(l).String()
		}
		c.sourceTracker.TrackConfig(composed, l.Source(), loaderType, c.env, "")

		// Track file for incremental reload.
		_ = c.fileTracker.Track(l.Source())

		configs = append(configs, composed)
	}

	c.mergedConfig = merge.MergeAll(c.merger, configs...)
	c.envConfig = c.envHandler.Resolve(c.mergedConfig, c.env)

	// Re-track the env-resolved config so introspection uses resolved keys
	// (e.g., "database.host" not "production.database.host").
	c.sourceTracker.TrackConfig(c.envConfig, "(resolved)", "EnvironmentHandler", c.env, "")

	return nil
}

// ---------------------------------------------------------------------------
// Access methods
// ---------------------------------------------------------------------------

// Get retrieves a value by dot-separated key path. Hooks are applied to leaf values.
func (c *Config[T]) Get(keyPath string) (any, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := dictutil.GetNested(c.envConfig, keyPath)
	if !ok {
		if c.opts.SysenvFallback {
			if envVal, found := c.lookupSysenv(keyPath); found {
				return c.hookProcessor.Process(keyPath, envVal), nil
			}
		}
		return nil, NewNotFoundError(keyPath, dictutil.FlatKeys(c.envConfig))
	}

	if _, isMap := val.(map[string]any); isMap {
		return val, nil
	}
	return c.hookProcessor.Process(keyPath, val), nil
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
func (c *Config[T]) GetBool(keyPath string) (bool, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return false, err
	}
	if b, ok := val.(bool); ok {
		return b, nil
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

// Has checks if a key exists in the configuration.
func (c *Config[T]) Has(keyPath string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return dictutil.HasNested(c.envConfig, keyPath)
}

// SetOption controls behavior of Set.
type SetOption func(*setOpts)
type setOpts struct{ allowOverride bool }

// WithOverride allows or prevents overwriting existing keys. Default: true.
func WithOverride(v bool) SetOption {
	return func(o *setOpts) { o.allowOverride = v }
}

// Set sets a value by dot-separated key path. Thread-safe, respects frozen state.
// Pass WithOverride(false) to raise an error if the key already exists.
func (c *Config[T]) Set(keyPath string, value any, opts ...SetOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	if err := dictutil.SetNested(c.envConfig, keyPath, value); err != nil {
		return err
	}
	_ = dictutil.SetNested(c.mergedConfig, keyPath, value)
	c.validatedModel = nil

	return nil
}

// Keys returns all dot-separated leaf key paths.
func (c *Config[T]) Keys(prefix ...string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	var keys []string
	if p != "" {
		keys = dictutil.FlatKeysWithPrefix(c.envConfig, p)
	} else {
		keys = dictutil.FlatKeys(c.envConfig)
	}
	sort.Strings(keys)
	return keys
}

// ToDict returns the effective configuration as a plain map.
func (c *Config[T]) ToDict() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.envConfig != nil {
		return c.envConfig
	}
	return c.mergedConfig
}

// ---------------------------------------------------------------------------
// Introspection methods
// ---------------------------------------------------------------------------

// Explain returns detailed resolution information for a key.
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
		"value":          info.Value,
		"source":         info.SourceFile,
		"loader_type":    info.LoaderType,
		"environment":    c.env,
		"override_count": info.OverrideCount,
	}

	if len(info.History) > 0 {
		var history []map[string]any
		for _, h := range info.History {
			history = append(history, map[string]any{
				"value": h.Value, "source": h.Source, "loader_type": h.LoaderType,
			})
		}
		result["override_history"] = history
	}

	// Current value from live config.
	if val, ok := dictutil.GetNested(c.envConfig, keyPath); ok {
		result["current_value"] = val
	}

	return result
}

// Schema returns schema information for a key.
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
	result["value"] = val
	result["type"] = fmt.Sprintf("%T", val)

	return result
}

// Layers returns the layer stack showing each source and its keys.
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

		loaderType := reflect.TypeOf(l).Elem().Name()
		if loaderType == "" {
			loaderType = reflect.TypeOf(l).String()
		}

		keys := c.sourceTracker.FindKeysFromSource(source)
		layers = append(layers, map[string]any{
			"source":      source,
			"loader_type": loaderType,
			"keys":        keys,
			"key_count":   len(keys),
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

// ReloadOption configures Reload behavior.
type ReloadOption func(*reloadOpts)
type reloadOpts struct {
	validate    *bool
	dryRun      bool
	incremental bool
}

// WithReloadValidate overrides validate_on_load for this reload.
func WithReloadValidate(v bool) ReloadOption {
	return func(o *reloadOpts) { o.validate = &v }
}

// WithDryRun loads and validates without applying changes.
func WithDryRun(v bool) ReloadOption {
	return func(o *reloadOpts) { o.dryRun = v }
}

// WithIncremental only reloads files that have changed (based on mtime+hash).
func WithIncremental(v bool) ReloadOption {
	return func(o *reloadOpts) { o.incremental = v }
}

// Reload reloads all configurations from their sources.
func (c *Config[T]) Reload(ctx context.Context, opts ...ReloadOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.frozen {
		return NewFrozenError("Reload")
	}

	ro := reloadOpts{incremental: true}
	for _, o := range opts {
		o(&ro)
	}

	// Incremental check.
	if ro.incremental {
		var paths []string
		for _, l := range c.loaders {
			paths = append(paths, l.Source())
		}
		changed := c.fileTracker.GetChangedFiles(paths)
		if len(changed) == 0 {
			return nil // nothing changed
		}
	}

	// Save old state for rollback.
	oldEnv := copyMap(c.envConfig)
	oldMerged := copyMap(c.mergedConfig)
	start := time.Now()

	if err := c.load(ctx); err != nil {
		// Rollback.
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		return err
	}

	duration := time.Since(start)

	// Record metrics.
	if c.observer != nil {
		c.observer.RecordReload(duration)
	}
	if c.eventEmitter != nil {
		c.eventEmitter.Emit("reload", c.envConfig, duration)
	}

	// Validate.
	shouldValidate := c.opts.ValidateOnLoad
	if ro.validate != nil {
		shouldValidate = *ro.validate
	}
	if shouldValidate {
		c.validatedModel = nil
		if _, err := validate.DecodeAndValidate[T](c.envConfig); err != nil {
			c.envConfig = oldEnv
			c.mergedConfig = oldMerged
			return NewValidationError([]string{err.Error()}, err)
		}
	}

	// Dry run: roll back.
	if ro.dryRun {
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		c.logger.Info("dry-run reload completed, changes not applied")
		return nil
	}

	c.validatedModel = nil
	c.notifyChanges(oldEnv, c.envConfig)

	if c.observer != nil {
		c.observer.RecordChange()
	}
	if c.eventEmitter != nil {
		c.eventEmitter.Emit("change", oldEnv, c.envConfig)
	}

	return nil
}

// Extend adds an additional loader at runtime and merges its config.
func (c *Config[T]) Extend(ctx context.Context, l Loader) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.frozen {
		return NewFrozenError("Extend")
	}

	data, err := l.Load(ctx)
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}

	c.loaders = append(c.loaders, l)
	c.mergedConfig = c.merger.Merge(c.mergedConfig, data)
	// Merge directly into envConfig too, since the new loader may be flat
	// (no environment sections) and should apply on top of the resolved config.
	c.envConfig = c.merger.Merge(c.envConfig, data)
	c.validatedModel = nil

	loaderType := reflect.TypeOf(l).Elem().Name()
	c.sourceTracker.TrackConfig(data, l.Source(), loaderType, c.env, "")

	return nil
}

// Override temporarily overrides configuration values.
// Returns a restore function that must be called (typically via defer) to revert.
func (c *Config[T]) Override(overrides map[string]any) (restore func(), err error) {
	c.mu.Lock()

	oldEnv := copyMap(c.envConfig)
	oldMerged := copyMap(c.mergedConfig)
	wasFrozen := c.frozen
	c.frozen = false

	for k, v := range overrides {
		_ = dictutil.SetNested(c.envConfig, k, v)
		_ = dictutil.SetNested(c.mergedConfig, k, v)
	}
	c.validatedModel = nil
	c.mu.Unlock()

	restore = func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		c.frozen = wasFrozen
		c.validatedModel = nil
	}
	return restore, nil
}

// Export serializes the config to the given format ("json", "yaml", "toml").
// If outputPath is provided, also writes to that file.
func (c *Config[T]) Export(format string, outputPath ...string) ([]byte, error) {
	c.mu.RLock()
	data := c.envConfig
	c.mu.RUnlock()

	var result []byte
	var err error

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

// OnChange registers a callback that fires when configuration values change after reload.
func (c *Config[T]) OnChange(fn func(key string, oldVal, newVal any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.changeCallbacks = append(c.changeCallbacks, fn)
}

// HookProcessor returns the hook processor for registering custom hooks.
func (c *Config[T]) HookProcessor() *hook.Processor { return c.hookProcessor }

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

// EnableVersioning enables config versioning with snapshot persistence.
func (c *Config[T]) EnableVersioning(storagePath string, maxVersions int) *observe.VersionManager {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager(storagePath, maxVersions)
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

// SaveVersion saves the current config as an immutable version snapshot.
func (c *Config[T]) SaveVersion(metadata map[string]any) (*observe.Version, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.versionMgr == nil {
		c.mu.RUnlock()
		c.EnableVersioning("", 0)
		c.mu.RLock()
	}
	return c.versionMgr.SaveVersion(c.envConfig, metadata)
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

	c.envConfig = v.Config
	c.mergedConfig = v.Config
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
	var files []string
	for _, l := range c.loaders {
		files = append(files, l.Source())
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

// ---------------------------------------------------------------------------
// Typed access
// ---------------------------------------------------------------------------

// Typed decodes the configuration into a typed struct T and validates it.
func (c *Config[T]) Typed() (*T, error) {
	c.mu.RLock()
	if c.validatedModel != nil {
		defer c.mu.RUnlock()
		return c.validatedModel, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.validatedModel != nil {
		return c.validatedModel, nil
	}

	model, err := validate.DecodeAndValidate[T](c.envConfig)
	if err != nil {
		return nil, NewValidationError([]string{err.Error()}, err)
	}
	c.validatedModel = model
	return model, nil
}

// String returns a human-readable representation.
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

func (c *Config[T]) notifyChanges(oldConfig, newConfig map[string]any) {
	if len(c.changeCallbacks) == 0 {
		return
	}
	flat := dictutil.Flatten(newConfig)
	oldFlat := dictutil.Flatten(oldConfig)
	for key, newVal := range flat {
		oldVal := oldFlat[key]
		if fmt.Sprintf("%v", oldVal) != fmt.Sprintf("%v", newVal) {
			for _, cb := range c.changeCallbacks {
				func() {
					defer func() { _ = recover() }()
					cb(key, oldVal, newVal)
				}()
			}
		}
	}
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
func applySelfConfig(opts *options) error {
	settings, err := selfconfig.Read(".")
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
		opts.OnError = ErrorPolicy(settings.OnError)
	}
	if !opts.isSet("loaders") && len(settings.DefaultFiles) > 0 {
		for _, f := range settings.DefaultFiles {
			opts.Loaders = append(opts.Loaders, &fileAutoLoader{path: f})
		}
	}
	return nil
}

// fileAutoLoader is a minimal loader that auto-detects format from extension.
type fileAutoLoader struct{ path string }

func (l *fileAutoLoader) Source() string { return l.path }
func (l *fileAutoLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, NewLoadError(l.path, err)
	}
	format := formatparse.FromExtension(l.path)
	var result map[string]any
	switch format {
	case formatparse.FormatYAML:
		err = yaml.Unmarshal(data, &result)
	case formatparse.FormatJSON:
		err = json.Unmarshal(data, &result)
	default:
		err = yaml.Unmarshal(data, &result)
	}
	if err != nil {
		return nil, NewFormatError(l.path, string(format), err)
	}
	return result, nil
}
