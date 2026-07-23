package confii_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/hook"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Wave 20 Validated Gap G04: Self-config exposes nonfunctional settings.
//
// selfconfig.Settings parses default_prefix, log_level, sources, and secrets
// (selfconfig/reader.go:28-47), but applySelfConfig never applies them. The
// fix wires each parsed-but-unused setting into the option chain so that a
// .confii.yaml declaring these values has the same end-to-end observable as
// the equivalent explicit option calls — preserving explicit > self-config
// priority across all four.
// =============================================================================

// chdirToSelfConfig moves into a tmp dir containing the supplied
// .confii.yaml content, clears the selfconfig CWD cache so the new file
// is the one that's read, and registers cleanup so the working
// directory and cache are restored. Returns the temp dir path.
func chdirToSelfConfig(t *testing.T, confiiYAML string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(confiiYAML), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	return dir
}

// TestDefaultPrefix_AppliesEnvPrefix pins the documented contract that
// self-config `default_prefix: APP` is functionally equivalent to an
// explicit confii.WithEnvPrefix("APP") call: setting APP_HOST in the OS
// environment must surface as cfg.Get("host") via the loader pipeline
// (G03's envPrefixAutoLoader wiring), not merely as a sysenv-fallback
// shortcut.
func TestDefaultPrefix_AppliesEnvPrefix(t *testing.T) {
	chdirToSelfConfig(t, "default_prefix: APP\n")
	t.Setenv("APP_HOST", "example.com")

	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err,
		"G04: default_prefix in self-config must wire APP_HOST via the loader pipeline")
	assert.Equal(t, "example.com", host)

	// The auto-installed env loader must appear in cfg.Layers() under
	// the canonical environment:APP source identifier.
	layers := cfg.Layers()
	var found bool
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			found = true
			break
		}
	}
	assert.True(t, found,
		"G04: self-config default_prefix must surface as an environment:APP layer")
}

// TestDefaultPrefix_ExplicitWithEnvPrefix_Wins pins the explicit >
// self-config priority: an explicit confii.WithEnvPrefix("MYSVC") must
// suppress a self-config default_prefix:APP value end-to-end. Observable:
// MYSVC_HOST surfaces, APP_HOST does not.
func TestDefaultPrefix_ExplicitWithEnvPrefix_Wins(t *testing.T) {
	chdirToSelfConfig(t, "default_prefix: APP\n")
	t.Setenv("MYSVC_HOST", "from-mysvc")
	t.Setenv("APP_HOST", "from-app")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvPrefix("MYSVC"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "from-mysvc", host,
		"G04: explicit WithEnvPrefix must beat self-config default_prefix")

	// And the env:APP layer must NOT have been installed: only the
	// explicit MYSVC layer is present.
	layers := cfg.Layers()
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			t.Errorf("G04: explicit WithEnvPrefix(MYSVC) must suppress self-config default_prefix:APP layer; got %v", layers)
		}
	}
}

// TestLogLevel_ConstructsLogger pins that self-config `log_level:
// debug` builds an *slog.Logger at debug level and assigns it to
// opts.Logger. Observable: a log record emitted at debug level by the
// resulting Config's logger is captured by the underlying handler. We
// verify by reading back cfg.Logger() and confirming it accepts a Debug
// record (handler.Enabled returns true at slog.LevelDebug).
func TestLogLevel_ConstructsLogger(t *testing.T) {
	chdirToSelfConfig(t, "log_level: debug\n")

	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	lg := cfg.Logger()
	require.NotNil(t, lg, "G04: self-config log_level must produce a non-nil *slog.Logger")
	assert.True(t, lg.Handler().Enabled(context.Background(), slog.LevelDebug),
		"G04: log_level: debug must enable slog.LevelDebug on the constructed handler")
	// And it must NOT be the slog.Default() singleton — the self-
	// config-driven logger is a freshly constructed JSON handler at
	// the requested level.
	assert.NotSame(t, slog.Default(), lg,
		"G04: self-config log_level must construct a new logger, not return slog.Default()")
}

// TestLogLevel_InvalidString_TypedError pins the G07-style typed
// error contract: an unrecognised log_level surfaces as a typed
// *ConfigError wrapping ErrConfigLoad rather than being silently
// coerced or ignored. Caller can detect via errors.As / errors.Is.
func TestLogLevel_InvalidString_TypedError(t *testing.T) {
	chdirToSelfConfig(t, "log_level: bogus\n")

	_, err := confii.New[any](context.Background())
	require.Error(t, err)

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		"G04: invalid log_level must produce a typed *ConfigError; got %T (%v)", err, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad),
		"G04: invalid log_level error must wrap ErrConfigLoad")
	assert.Contains(t, err.Error(), "bogus",
		"G04: invalid log_level error must mention the offending value")
}

// TestLogLevel_ExplicitWithLogger_Wins pins the explicit >
// self-config priority for log_level: an explicit confii.WithLogger must
// be untouched by self-config log_level: debug. We supply a captured
// logger and assert cfg.Logger() returns the same pointer.
func TestLogLevel_ExplicitWithLogger_Wins(t *testing.T) {
	chdirToSelfConfig(t, "log_level: debug\n")

	var buf bytes.Buffer
	explicit := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLogger(explicit),
	)
	require.NoError(t, err)

	assert.Same(t, explicit, cfg.Logger(),
		"G04: explicit WithLogger must beat self-config log_level")
	// And the explicit logger's level must be unchanged: debug records
	// must be discarded.
	assert.False(t, cfg.Logger().Handler().Enabled(context.Background(), slog.LevelDebug),
		"G04: explicit WithLogger preserves its own level (LevelError); debug must be filtered out")
}

// TestSources_AppendsToLoaders pins that self-config `sources:` is
// translated into Loader instances and appended to opts.Loaders. The
// observable is two-fold: (1) cfg.Layers() lists the declared paths and
// (2) cfg.Get can read keys from the declared sources.
func TestSources_AppendsToLoaders(t *testing.T) {
	dir := chdirToSelfConfig(t, `
sources:
  - type: yaml
    path: extra.yaml
  - type: json
    path: extra.json
  - type: env
    prefix: APPCFG
`)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.yaml"),
		[]byte("yaml_only: from-yaml\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.json"),
		[]byte(`{"json_only": "from-json"}`), 0644))
	t.Setenv("APPCFG_ENV_ONLY", "from-env")

	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	yamlVal, err := cfg.Get("yaml_only")
	require.NoError(t, err, "G04: sources[yaml] must be loaded")
	assert.Equal(t, "from-yaml", yamlVal)

	jsonVal, err := cfg.Get("json_only")
	require.NoError(t, err, "G04: sources[json] must be loaded")
	assert.Equal(t, "from-json", jsonVal)

	envVal, err := cfg.Get("env_only")
	require.NoError(t, err, "G04: sources[env, prefix=APPCFG] must be loaded")
	assert.Equal(t, "from-env", envVal)

	// And cfg.Layers() must expose every declared source identifier.
	layers := cfg.Layers()
	sources := make(map[string]bool)
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok {
			sources[src] = true
		}
	}
	assert.True(t, sources["extra.yaml"], "G04: cfg.Layers() must list extra.yaml; got %v", sources)
	assert.True(t, sources["extra.json"], "G04: cfg.Layers() must list extra.json; got %v", sources)
	assert.True(t, sources["environment:APPCFG"], "G04: cfg.Layers() must list environment:APPCFG; got %v", sources)
}

// TestSources_UnknownType_TypedError pins that an unsupported source
// type surfaces as a typed *ConfigError instead of silently dropping
// the declared loader.
func TestSources_UnknownType_TypedError(t *testing.T) {
	chdirToSelfConfig(t, `
sources:
  - type: redis
    path: localhost:6379
`)

	_, err := confii.New[any](context.Background())
	require.Error(t, err)

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		"G04: unknown source type must produce a typed *ConfigError; got %T", err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad),
		"G04: unknown source type must wrap ErrConfigLoad")
}

// TestSecrets_RegistersStores pins that self-config `secrets:` with
// `provider: env` installs a global hook on the Config that resolves
// ${secret:KEY} placeholders against OS environment variables. The
// observable: a value containing ${secret:db/password} (read via
// cfg.GetCtx) returns the resolved env-var value.
func TestSecrets_RegistersStores(t *testing.T) {
	dir := chdirToSelfConfig(t, `
default_files:
  - app.yaml
secrets:
  provider: env
  prefix: APP_
`)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yaml"),
		[]byte("db:\n  password: \"${secret:db/password}\"\n"), 0644))
	t.Setenv("APP_DB_PASSWORD", "s3cr3t")

	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	pw, err := cfg.GetCtx(context.Background(), "db.password")
	require.NoError(t, err,
		"G04: ${secret:db/password} must resolve via the self-config env secret hook")
	assert.Equal(t, "s3cr3t", pw)
}

// TestSecrets_UnknownProvider_TypedError pins that an unrecognised
// secrets.provider surfaces as a typed *ConfigError so operator typos
// in .confii.yaml fail loudly instead of silently installing a no-op
// hook.
func TestSecrets_UnknownProvider_TypedError(t *testing.T) {
	chdirToSelfConfig(t, `
secrets:
  provider: bogusvault
`)

	_, err := confii.New[any](context.Background())
	require.Error(t, err)

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		"G04: unknown secrets.provider must produce a typed *ConfigError; got %T", err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad),
		"G04: unknown secrets.provider must wrap ErrConfigLoad")
	assert.Contains(t, err.Error(), "bogusvault")
}

// TestAllSettings_NoOpWhenAlreadyExplicit is the integration
// regression: with all four parsed-but-unused settings declared in
// self-config AND all four also explicitly overridden via constructor
// options, the explicit values must win across the board. Pin via
// distinct observables for each:
//
//  1. default_prefix: APP   vs WithEnvPrefix("WINNER")  → loader is
//     environment:WINNER (NOT environment:APP).
//  2. log_level: debug      vs WithLogger(explicit @Error) → cfg.Logger()
//     is the explicit pointer, debug records are filtered.
//  3. sources: [yaml extra] vs WithLoaders(stub)         → cfg.Layers()
//     lists the stub source, NOT extra.yaml.
//  4. secrets: env          vs WithSecretHook(record)    → ${secret:K}
//     in a config value is processed by the explicit hook (it records
//     the call and returns "explicit-resolved"); the env-store hook is
//     not installed (env var holding the secret is unset, so an
//     accidental fallback would error).
func TestAllSettings_NoOpWhenAlreadyExplicit(t *testing.T) {
	dir := chdirToSelfConfig(t, `
default_prefix: APP
log_level: debug
sources:
  - type: yaml
    path: extra.yaml
secrets:
  provider: env
  prefix: SELF_
`)

	// Self-config sources and env vars: WINNER_HOST and APP_HOST
	// disambiguate which loader actually ran. SELF_DB_PASSWORD is
	// intentionally UNSET so a leak of the env-store hook would be
	// observable as a missing-secret error rather than a silent pass.
	t.Setenv("WINNER_HOST", "from-explicit-prefix")
	t.Setenv("APP_HOST", "from-self-config-prefix")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.yaml"),
		[]byte("yaml_only: from-self-config-source\n"), 0644))

	// (1) Explicit env prefix.
	// (2) Explicit logger at LevelError.
	var logBuf bytes.Buffer
	explicitLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))

	// (3) Explicit loader supplying a different key.
	stub := &g04ValueLoader{src: "stub:explicit", data: map[string]any{
		"explicit_only": "from-explicit-loader",
		"db":            map[string]any{"password": "${secret:db/password}"},
	}}

	// (4) Explicit secret hook that records every call and returns
	// "explicit-resolved" without touching the OS environment.
	var hookCalls int
	explicitHook := hook.FuncCtx(func(_ context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok {
			return value, nil
		}
		if s == "${secret:db/password}" {
			hookCalls++
			return "explicit-resolved", nil
		}
		return value, nil
	})

	// Order matters: WithLoaders must come BEFORE WithEnvPrefix because
	// WithLoaders replaces o.Loaders. With this order, the auto-installed
	// env loader from WithEnvPrefix is appended to the (post-replace)
	// loader chain so cfg.Get("host") can resolve.
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(stub),
		confii.WithEnvPrefix("WINNER"),
		confii.WithLogger(explicitLogger),
		confii.WithSecretHook(explicitHook),
	)
	require.NoError(t, err)

	// (1) Explicit env prefix wins: WINNER_HOST resolves, APP_HOST
	// does NOT (no environment:APP layer was installed).
	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "from-explicit-prefix", host,
		"G04: explicit WithEnvPrefix must beat self-config default_prefix")

	layers := cfg.Layers()
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok {
			assert.NotEqual(t, "environment:APP", src,
				"G04: self-config default_prefix layer must not be installed when explicit WithEnvPrefix is supplied")
			assert.NotEqual(t, "extra.yaml", src,
				"G04: self-config sources layer must not be installed when explicit WithLoaders is supplied")
		}
	}

	// (2) Explicit logger wins.
	assert.Same(t, explicitLogger, cfg.Logger())
	assert.False(t, cfg.Logger().Handler().Enabled(context.Background(), slog.LevelDebug),
		"G04: explicit logger preserves LevelError; self-config debug must NOT raise the level")

	// (3) Explicit loader's keys are present; self-config sources
	// keys (yaml_only) are NOT.
	exp, err := cfg.Get("explicit_only")
	require.NoError(t, err)
	assert.Equal(t, "from-explicit-loader", exp)
	_, err = cfg.Get("yaml_only")
	require.Error(t, err,
		"G04: when WithLoaders is explicit, self-config sources must NOT also be loaded")

	// (4) Explicit secret hook wins: cfg.GetCtx must use the recorded
	// hook (returning "explicit-resolved") rather than the env-store
	// hook (which would error on missing SELF_DB_PASSWORD).
	pw, err := cfg.GetCtx(context.Background(), "db.password")
	require.NoError(t, err,
		"G04: explicit WithSecretHook must replace the self-config env-store hook")
	assert.Equal(t, "explicit-resolved", pw)
	assert.GreaterOrEqual(t, hookCalls, 1,
		"G04: explicit secret hook must have been invoked at least once for the placeholder")
}

// g04ValueLoader is a minimal in-memory Loader for G04's
// explicit-loader-wins regression: returns a fixed map with a stable
// Source identifier so cfg.Layers() can be asserted.
type g04ValueLoader struct {
	src  string
	data map[string]any
}

func (l *g04ValueLoader) Source() string { return l.src }
func (l *g04ValueLoader) Load(_ context.Context) (map[string]any, error) {
	return l.data, nil
}

// Compile-time assertion that g04ValueLoader implements confii.Loader.
var _ confii.Loader = (*g04ValueLoader)(nil)
