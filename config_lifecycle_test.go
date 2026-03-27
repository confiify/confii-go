package confii

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// stubLoader is a minimal in-memory Loader for tests.
// ---------------------------------------------------------------------------

type stubLoader struct {
	source string
	data   map[string]any
	err    error
}

func (s *stubLoader) Load(_ context.Context) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.data, nil
}

func (s *stubLoader) Source() string { return s.source }

// helper: write a temp YAML file and return its path.
func writeTempYAML(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0644))
	return p
}

// helper: create a Config[any] with stub data.
func newTestConfig(t *testing.T, data map[string]any, opts ...Option) *Config[any] {
	t.Helper()
	all := []Option{
		WithLoaders(&stubLoader{source: "stub", data: data}),
	}
	all = append(all, opts...)
	cfg, err := New[any](context.Background(), all...)
	require.NoError(t, err)
	return cfg
}

// =========================================================================
// 1-4. Reload tests
// =========================================================================

func TestReload_Full(t *testing.T) {
	// Use a real temp YAML file so reload re-reads from disk.
	path := writeTempYAML(t, "cfg.yaml", "app:\n  name: before\n")
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
	)
	require.NoError(t, err)

	v, err := cfg.Get("app.name")
	require.NoError(t, err)
	assert.Equal(t, "before", v)

	// Overwrite file and do a full (non-incremental) reload.
	require.NoError(t, os.WriteFile(path, []byte("app:\n  name: after\n"), 0644))

	err = cfg.Reload(context.Background(), WithIncremental(false))
	require.NoError(t, err)

	v, err = cfg.Get("app.name")
	require.NoError(t, err)
	assert.Equal(t, "after", v)
}

func TestReload_Incremental_NoChange(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "key: value\n")
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
	)
	require.NoError(t, err)

	// Incremental reload with no file change should be a no-op.
	err = cfg.Reload(context.Background(), WithIncremental(true))
	require.NoError(t, err)

	v, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", v)
}

func TestReload_DryRun(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "key: original\n")
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
	)
	require.NoError(t, err)

	// Overwrite on disk.
	require.NoError(t, os.WriteFile(path, []byte("key: changed\n"), 0644))

	// Dry-run reload should NOT apply the new value.
	err = cfg.Reload(context.Background(), WithDryRun(true), WithIncremental(false))
	require.NoError(t, err)

	v, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "original", v)
}

func TestReload_WithValidate(t *testing.T) {
	type AppCfg struct {
		Key string `mapstructure:"key" validate:"required"`
	}

	path := writeTempYAML(t, "cfg.yaml", "key: hello\n")
	cfg, err := New[AppCfg](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
	)
	require.NoError(t, err)

	// Reload with validation enabled -- should succeed.
	err = cfg.Reload(context.Background(), WithReloadValidate(true), WithIncremental(false))
	require.NoError(t, err)
}

func TestReload_Frozen(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})
	cfg.Freeze()

	err := cfg.Reload(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigFrozen))
}

func TestReload_ChangeCallback(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "key: old\n")
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
	)
	require.NoError(t, err)

	var changedKeys []string
	cfg.OnChange(func(key string, oldVal, newVal any) {
		changedKeys = append(changedKeys, key)
	})

	require.NoError(t, os.WriteFile(path, []byte("key: new\n"), 0644))
	require.NoError(t, cfg.Reload(context.Background(), WithIncremental(false)))

	assert.Contains(t, changedKeys, "key")
}

// =========================================================================
// 5. Extend
// =========================================================================

func TestExtend(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})

	extra := &stubLoader{
		source: "extra",
		data:   map[string]any{"b": 2},
	}
	err := cfg.Extend(context.Background(), extra)
	require.NoError(t, err)

	v, err := cfg.Get("b")
	require.NoError(t, err)
	assert.Equal(t, 2, v)
}

func TestExtend_Frozen(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})
	cfg.Freeze()

	err := cfg.Extend(context.Background(), &stubLoader{source: "x", data: map[string]any{"b": 2}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigFrozen))
}

func TestExtend_NilData(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})

	err := cfg.Extend(context.Background(), &stubLoader{source: "empty", data: nil})
	require.NoError(t, err)
	// Original data still present.
	assert.True(t, cfg.Has("a"))
}

func TestExtend_LoaderError(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})

	err := cfg.Extend(context.Background(), &stubLoader{
		source: "bad",
		err:    errors.New("boom"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// =========================================================================
// 6. StopWatching - should not panic when no watcher
// =========================================================================

func TestStopWatching_NoWatcher(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})
	assert.NotPanics(t, func() {
		cfg.StopWatching()
	})
}

func TestStopWatching_AfterStart(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "k: v\n")
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
		WithDynamicReloading(true),
	)
	require.NoError(t, err)

	// watcher should be set (or nil if fsnotify fails on temp paths,
	// but StopWatching must not panic either way).
	assert.NotPanics(t, func() {
		cfg.StopWatching()
	})
}

// =========================================================================
// 7. startWatching via WithDynamicReloading(true)
// =========================================================================

func TestStartWatching_ViaOption(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "k: v\n")
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
		WithDynamicReloading(true),
	)
	require.NoError(t, err)

	// Cleanup: stop the watcher so goroutines don't leak.
	defer cfg.StopWatching()

	v, err := cfg.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

// =========================================================================
// 8-13. Builder methods
// =========================================================================

func TestBuilder_AddLoaders(t *testing.T) {
	l1 := &stubLoader{source: "s1", data: map[string]any{"a": 1}}
	l2 := &stubLoader{source: "s2", data: map[string]any{"b": 2}}
	cfg, err := NewBuilder[any]().
		AddLoaders(l1, l2).
		Build(context.Background())
	require.NoError(t, err)

	assert.True(t, cfg.Has("a"))
	assert.True(t, cfg.Has("b"))
}

func TestBuilder_EnableDisableDynamicReloading(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "k: v\n")

	cfg, err := NewBuilder[any]().
		AddLoader(&fileAutoLoader{path: path}).
		EnableDynamicReloading().
		DisableDynamicReloading(). // disable overrides enable
		Build(context.Background())
	require.NoError(t, err)
	defer cfg.StopWatching()

	// watcher should be nil because we disabled dynamic reloading.
	assert.Nil(t, cfg.watcher)
}

func TestBuilder_EnableDisableEnvExpander(t *testing.T) {
	t.Setenv("LIFECYCLE_TEST_VAR", "expanded")

	cfg, err := NewBuilder[any]().
		AddLoader(&stubLoader{source: "s", data: map[string]any{"k": "${LIFECYCLE_TEST_VAR}"}}).
		EnableEnvExpander().
		Build(context.Background())
	require.NoError(t, err)

	v, err := cfg.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "expanded", v)

	// Now with disabled env expander.
	cfg2, err := NewBuilder[any]().
		AddLoader(&stubLoader{source: "s", data: map[string]any{"k": "${LIFECYCLE_TEST_VAR}"}}).
		DisableEnvExpander().
		Build(context.Background())
	require.NoError(t, err)

	v2, err := cfg2.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "${LIFECYCLE_TEST_VAR}", v2)
}

func TestBuilder_EnableDisableTypeCasting(t *testing.T) {
	cfg, err := NewBuilder[any]().
		AddLoader(&stubLoader{source: "s", data: map[string]any{"port": "8080"}}).
		EnableTypeCasting().
		Build(context.Background())
	require.NoError(t, err)

	v, err := cfg.Get("port")
	require.NoError(t, err)
	// With type casting enabled, "8080" should become int.
	assert.Equal(t, 8080, v)

	cfg2, err := NewBuilder[any]().
		AddLoader(&stubLoader{source: "s", data: map[string]any{"port": "8080"}}).
		DisableTypeCasting().
		Build(context.Background())
	require.NoError(t, err)

	v2, err := cfg2.Get("port")
	require.NoError(t, err)
	// With type casting disabled, "8080" stays string (env expander still runs but doesn't change it).
	assert.IsType(t, "", v2)
}

func TestBuilder_EnableDisableDeepMerge(t *testing.T) {
	l1 := &stubLoader{source: "s1", data: map[string]any{
		"db": map[string]any{"host": "h1", "port": 1234},
	}}
	l2 := &stubLoader{source: "s2", data: map[string]any{
		"db": map[string]any{"host": "h2"},
	}}

	// Deep merge: port should survive.
	cfg, err := NewBuilder[any]().
		AddLoaders(l1, l2).
		EnableDeepMerge().
		Build(context.Background())
	require.NoError(t, err)

	assert.True(t, cfg.Has("db.port"))

	// Shallow merge: l2 replaces entire "db" map, so port is lost.
	cfg2, err := NewBuilder[any]().
		AddLoaders(l1, l2).
		DisableDeepMerge().
		Build(context.Background())
	require.NoError(t, err)

	assert.False(t, cfg2.Has("db.port"))
}

func TestBuilder_EnableDebug(t *testing.T) {
	cfg, err := NewBuilder[any]().
		AddLoader(&stubLoader{source: "s", data: map[string]any{"k": "v"}}).
		EnableDebug().
		Build(context.Background())
	require.NoError(t, err)

	assert.True(t, cfg.opts.DebugMode)
}

func TestBuilder_WithSchemaValidation(t *testing.T) {
	type Schema struct {
		K string `mapstructure:"k" validate:"required"`
	}

	cfg, err := NewBuilder[Schema]().
		AddLoader(&stubLoader{source: "s", data: map[string]any{"k": "v"}}).
		WithSchemaValidation(Schema{}, true).
		Build(context.Background())
	require.NoError(t, err)

	assert.True(t, cfg.opts.ValidateOnLoad)
	assert.True(t, cfg.opts.StrictValidation)
	assert.NotNil(t, cfg.opts.Schema)
}

// =========================================================================
// 15-16. WithMergeStrategyOption / WithMergeStrategyMap
// =========================================================================

func TestWithMergeStrategyOption(t *testing.T) {
	l1 := &stubLoader{source: "s1", data: map[string]any{
		"db": map[string]any{"host": "h1", "port": 1234},
	}}
	l2 := &stubLoader{source: "s2", data: map[string]any{
		"db": map[string]any{"host": "h2"},
	}}

	cfg, err := New[any](context.Background(),
		WithLoaders(l1, l2),
		WithMergeStrategyOption(StrategyReplace),
	)
	require.NoError(t, err)

	// With Replace strategy, l2's "db" replaces l1's "db" entirely.
	v, err := cfg.Get("db.host")
	require.NoError(t, err)
	assert.Equal(t, "h2", v)

	// "port" should be gone because Replace replaces the entire "db" map.
	assert.False(t, cfg.Has("db.port"))
}

func TestWithMergeStrategyMap(t *testing.T) {
	opts := defaultOptions()
	fn := WithMergeStrategyMap(map[string]MergeStrategy{
		"special": StrategyReplace,
	})
	fn(&opts)

	assert.NotNil(t, opts.MergeStrategyMap)
	assert.Equal(t, StrategyReplace, opts.MergeStrategyMap["special"])
	assert.True(t, opts.isSet("merge_strategy_map"))
}

// =========================================================================
// 17. WithSchema / WithSchemaPath
// =========================================================================

func TestWithSchema(t *testing.T) {
	opts := defaultOptions()
	s := struct{ Name string }{}
	fn := WithSchema(s)
	fn(&opts)

	assert.Equal(t, s, opts.Schema)
	assert.True(t, opts.isSet("schema"))
}

func TestWithSchemaPath(t *testing.T) {
	opts := defaultOptions()
	fn := WithSchemaPath("/some/path.json")
	fn(&opts)

	assert.Equal(t, "/some/path.json", opts.SchemaPath)
	assert.True(t, opts.isSet("schema_path"))
}

// =========================================================================
// 18. WithValidateOnLoad / WithStrictValidation
// =========================================================================

func TestWithValidateOnLoad(t *testing.T) {
	opts := defaultOptions()
	WithValidateOnLoad(true)(&opts)

	assert.True(t, opts.ValidateOnLoad)
	assert.True(t, opts.isSet("validate_on_load"))
}

func TestWithStrictValidation(t *testing.T) {
	opts := defaultOptions()
	WithStrictValidation(true)(&opts)

	assert.True(t, opts.StrictValidation)
	assert.True(t, opts.isSet("strict_validation"))
}

// =========================================================================
// 19. WithOnError
// =========================================================================

func TestWithOnError(t *testing.T) {
	opts := defaultOptions()
	WithOnError(ErrorPolicyWarn)(&opts)

	assert.Equal(t, ErrorPolicyWarn, opts.OnError)
	assert.True(t, opts.isSet("on_error"))
}

func TestWithOnError_Ignore(t *testing.T) {
	opts := defaultOptions()
	WithOnError(ErrorPolicyIgnore)(&opts)

	assert.Equal(t, ErrorPolicyIgnore, opts.OnError)
}

func TestWithOnError_LoaderError_Warn(t *testing.T) {
	badLoader := &stubLoader{source: "bad", err: errors.New("fail")}
	cfg, err := New[any](context.Background(),
		WithLoaders(badLoader),
		WithOnError(ErrorPolicyWarn),
	)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestWithOnError_LoaderError_Raise(t *testing.T) {
	badLoader := &stubLoader{source: "bad", err: errors.New("fail")}
	_, err := New[any](context.Background(),
		WithLoaders(badLoader),
		WithOnError(ErrorPolicyRaise),
	)
	require.Error(t, err)
}

// =========================================================================
// 20. WithDebugMode
// =========================================================================

func TestWithDebugMode(t *testing.T) {
	opts := defaultOptions()
	WithDebugMode(true)(&opts)

	assert.True(t, opts.DebugMode)
	assert.True(t, opts.isSet("debug_mode"))

	WithDebugMode(false)(&opts)
	assert.False(t, opts.DebugMode)
}

// =========================================================================
// 21. WithLogger
// =========================================================================

func TestWithLogger(t *testing.T) {
	custom := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opts := defaultOptions()
	WithLogger(custom)(&opts)

	assert.Equal(t, custom, opts.Logger)
	assert.True(t, opts.isSet("logger"))
}

func TestWithLogger_UsedInConfig(t *testing.T) {
	custom := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg, err := New[any](context.Background(),
		WithLogger(custom),
	)
	require.NoError(t, err)
	assert.Equal(t, custom, cfg.logger)
}

// =========================================================================
// Edge cases / extra coverage
// =========================================================================

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()
	assert.True(t, opts.UseEnvExpander)
	assert.True(t, opts.UseTypeCasting)
	assert.True(t, opts.DeepMerge)
	assert.Equal(t, ErrorPolicyRaise, opts.OnError)
	assert.NotNil(t, opts.Logger)
	assert.NotNil(t, opts.explicitlySet)
}

func TestOptions_IsSet(t *testing.T) {
	opts := defaultOptions()
	assert.False(t, opts.isSet("env"))

	WithEnv("prod")(&opts)
	assert.True(t, opts.isSet("env"))
}

func TestReload_Incremental_WithChange(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "k: v1\n")
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
	)
	require.NoError(t, err)

	// Modify the file so incremental reload picks up the change.
	require.NoError(t, os.WriteFile(path, []byte("k: v2\n"), 0644))

	err = cfg.Reload(context.Background(), WithIncremental(true))
	require.NoError(t, err)

	v, err := cfg.Get("k")
	require.NoError(t, err)
	// The file changed so the new value should appear.
	assert.Equal(t, "v2", v)
}

func TestExtend_OverridesExisting(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"a": 1})

	err := cfg.Extend(context.Background(), &stubLoader{
		source: "override",
		data:   map[string]any{"a": 99},
	})
	require.NoError(t, err)

	v, err := cfg.Get("a")
	require.NoError(t, err)
	assert.Equal(t, 99, v)
}

func TestWithDynamicReloading_Option(t *testing.T) {
	opts := defaultOptions()
	WithDynamicReloading(true)(&opts)
	assert.True(t, opts.DynamicReloading)
	assert.True(t, opts.isSet("dynamic_reloading"))

	WithDynamicReloading(false)(&opts)
	assert.False(t, opts.DynamicReloading)
}

func TestWithEnvExpander_Option(t *testing.T) {
	opts := defaultOptions()
	WithEnvExpander(false)(&opts)
	assert.False(t, opts.UseEnvExpander)
	assert.True(t, opts.isSet("use_env_expander"))
}

func TestWithTypeCasting_Option(t *testing.T) {
	opts := defaultOptions()
	WithTypeCasting(false)(&opts)
	assert.False(t, opts.UseTypeCasting)
	assert.True(t, opts.isSet("use_type_casting"))
}

func TestWithDeepMerge_Option(t *testing.T) {
	opts := defaultOptions()
	WithDeepMerge(false)(&opts)
	assert.False(t, opts.DeepMerge)
	assert.True(t, opts.isSet("deep_merge"))
}

// =========================================================================
// applySelfConfig tests
// =========================================================================

func TestApplySelfConfig_WithConfiiYAML(t *testing.T) {
	dir := t.TempDir()
	confiiContent := `
default_environment: staging
env_switcher: APP_ENV
env_prefix: MYAPP
sysenv_fallback: true
deep_merge: false
use_env_expander: false
use_type_casting: false
validate_on_load: true
strict_validation: true
dynamic_reloading: true
freeze_on_load: true
debug_mode: true
schema_path: /path/to/schema.json
on_error: warn
default_files:
  - config.yaml
  - overrides.yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(confiiContent), 0644))

	// Write config files referenced by default_files.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("key: value\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "overrides.yaml"), []byte("key: overridden\n"), 0644))

	// Save current dir, chdir to tmp, then restore.
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	// Clear the selfconfig cache so it re-reads.
	selfconfig.ClearCache()
	defer selfconfig.ClearCache()

	opts := defaultOptions()
	err = applySelfConfig(&opts)
	require.NoError(t, err)

	assert.Equal(t, "staging", opts.Env)
	assert.Equal(t, "APP_ENV", opts.EnvSwitcher)
	assert.Equal(t, "MYAPP", opts.EnvPrefix)
	assert.True(t, opts.SysenvFallback)
	assert.False(t, opts.DeepMerge)
	assert.False(t, opts.UseEnvExpander)
	assert.False(t, opts.UseTypeCasting)
	assert.True(t, opts.ValidateOnLoad)
	assert.True(t, opts.StrictValidation)
	assert.True(t, opts.DynamicReloading)
	assert.True(t, opts.FreezeOnLoad)
	assert.True(t, opts.DebugMode)
	assert.Equal(t, "/path/to/schema.json", opts.SchemaPath)
	assert.Equal(t, ErrorPolicy("warn"), opts.OnError)
	assert.Len(t, opts.Loaders, 2)
}

func TestApplySelfConfig_ExplicitOverridesSelfConfig(t *testing.T) {
	dir := t.TempDir()
	confiiContent := `
default_environment: staging
env_prefix: FROMFILE
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(confiiContent), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	selfconfig.ClearCache()
	defer selfconfig.ClearCache()

	opts := defaultOptions()
	// Explicitly set env -- should NOT be overridden by self-config.
	WithEnv("production")(&opts)
	WithEnvPrefix("EXPLICIT")(&opts)

	err = applySelfConfig(&opts)
	require.NoError(t, err)

	assert.Equal(t, "production", opts.Env)
	assert.Equal(t, "EXPLICIT", opts.EnvPrefix)
}

func TestApplySelfConfig_NoConfigFile(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origDir) }()

	selfconfig.ClearCache()
	defer selfconfig.ClearCache()

	opts := defaultOptions()
	err = applySelfConfig(&opts)
	require.NoError(t, err)
	// No changes from defaults.
	assert.Equal(t, "", opts.Env)
}

// =========================================================================
// fileAutoLoader tests
// =========================================================================

func TestFileAutoLoader_YAML(t *testing.T) {
	path := writeTempYAML(t, "test.yaml", "key: value\nnested:\n  a: 1\n")
	l := &fileAutoLoader{path: path}
	assert.Equal(t, path, l.Source())

	data, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value", data["key"])
}

func TestFileAutoLoader_JSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"key": "jsonval"}`), 0644))

	l := &fileAutoLoader{path: p}
	data, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "jsonval", data["key"])
}

func TestFileAutoLoader_MissingFile(t *testing.T) {
	l := &fileAutoLoader{path: "/nonexistent/file.yaml"}
	data, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestFileAutoLoader_ParseError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(p, []byte("{invalid json"), 0644))

	l := &fileAutoLoader{path: p}
	_, err := l.Load(context.Background())
	assert.Error(t, err)
}

func TestFileAutoLoader_UnknownExtension(t *testing.T) {
	// Unknown extension defaults to YAML.
	dir := t.TempDir()
	p := filepath.Join(dir, "config.cfg")
	require.NoError(t, os.WriteFile(p, []byte("key: value\n"), 0644))

	l := &fileAutoLoader{path: p}
	data, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value", data["key"])
}

// =========================================================================
// copyMap tests
// =========================================================================

func TestCopyMap_DeepCopy(t *testing.T) {
	original := map[string]any{
		"a": 1,
		"b": map[string]any{
			"c": 2,
			"d": map[string]any{
				"e": 3,
			},
		},
	}

	copied := copyMap(original)

	// Mutate original nested map.
	original["b"].(map[string]any)["c"] = 99
	original["b"].(map[string]any)["d"].(map[string]any)["e"] = 99

	// Copied should be unchanged.
	assert.Equal(t, 2, copied["b"].(map[string]any)["c"])
	assert.Equal(t, 3, copied["b"].(map[string]any)["d"].(map[string]any)["e"])
}

func TestCopyMap_Empty(t *testing.T) {
	copied := copyMap(map[string]any{})
	assert.NotNil(t, copied)
	assert.Empty(t, copied)
}

func TestCopyMap_Nil(t *testing.T) {
	copied := copyMap(nil)
	assert.NotNil(t, copied)
	assert.Empty(t, copied)
}

// =========================================================================
// Explain with override history
// =========================================================================

func TestExplain_WithOverrideHistory(t *testing.T) {
	// Use two loaders with overlapping keys to create override history.
	l1 := &stubLoader{source: "first.yaml", data: map[string]any{"key": "from-first"}}
	l2 := &stubLoader{source: "second.yaml", data: map[string]any{"key": "from-second"}}

	cfg, err := New[any](context.Background(),
		WithLoaders(l1, l2),
		WithDebugMode(true),
	)
	require.NoError(t, err)

	info := cfg.Explain("key")
	assert.Equal(t, true, info["exists"])
	assert.Equal(t, "key", info["key"])
	assert.NotNil(t, info["current_value"])
}

// =========================================================================
// GetInt with int64 type
// =========================================================================

func TestGetInt_Int64(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"val": int64(42)})
	v, err := cfg.GetInt("val")
	require.NoError(t, err)
	assert.Equal(t, 42, v)
}

func TestGetFloat64_Int64(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"val": int64(42)})
	v, err := cfg.GetFloat64("val")
	require.NoError(t, err)
	assert.Equal(t, float64(42), v)
}

func TestGetFloat64_Int(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"val": 42})
	v, err := cfg.GetFloat64("val")
	require.NoError(t, err)
	assert.Equal(t, float64(42), v)
}

// =========================================================================
// Loader returning nil data (skip)
// =========================================================================

func TestLoad_NilDataLoader(t *testing.T) {
	cfg, err := New[any](context.Background(),
		WithLoaders(&stubLoader{source: "nil", data: nil}),
	)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Empty(t, cfg.Keys())
}

// =========================================================================
// Secret options from root package
// =========================================================================

func TestWithVersion_And_ResolveSecretOptions(t *testing.T) {
	opt := WithVersion("v2")
	o := ResolveSecretOptions(opt)
	assert.Equal(t, "v2", o.Version)
}

func TestResolveSecretOptions_Empty(t *testing.T) {
	o := ResolveSecretOptions()
	assert.Equal(t, "", o.Version)
}

// ===========================================================================
// Reload failure rollback (lines 645-650)
// ===========================================================================

type failOnReloadLoader struct {
	source    string
	data      map[string]any
	callCount int
}

func (l *failOnReloadLoader) Load(_ context.Context) (map[string]any, error) {
	l.callCount++
	if l.callCount > 1 {
		return nil, errors.New("simulated reload failure")
	}
	return l.data, nil
}

func (l *failOnReloadLoader) Source() string { return l.source }

func TestReload_FailureRollback(t *testing.T) {
	fLoader := &failOnReloadLoader{
		source: "fail-on-reload",
		data:   map[string]any{"key": "original"},
	}

	cfg, err := New[any](context.Background(),
		WithLoaders(fLoader),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)

	v, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "original", v)

	// Reload should fail because the loader now returns an error.
	err = cfg.Reload(context.Background(), WithIncremental(false))
	require.Error(t, err)

	// After failed reload, the original config should be preserved (rollback).
	v, err = cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "original", v)
}
