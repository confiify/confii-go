// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestEnvPrefix_AppliesEnvPrefix(t *testing.T) {
	chdirToSelfConfig(t, "env_prefix: APP\n")
	t.Setenv("APP_HOST", "example.com")

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err,
		":env_prefix in self-config must wire APP_HOST via the loader pipeline")
	assert.Equal(t, "example.com", host)

	layers := cfg.Layers()
	var found bool
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			found = true
			break
		}
	}
	assert.True(t, found,
		":self-config env_prefix must surface as an environment:APP layer")
}

func TestEnvPrefix_ExplicitWithEnvPrefix_Wins(t *testing.T) {
	chdirToSelfConfig(t, "env_prefix: APP\n")
	t.Setenv("MYSVC_HOST", "from-mysvc")
	t.Setenv("APP_HOST", "from-app")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("MYSVC"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "from-mysvc", host,
		":explicit WithEnvPrefix must beat self-config env_prefix")

	layers := cfg.Layers()
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			t.Errorf(":explicit WithEnvPrefix(MYSVC) must suppress self-config env_prefix:APP layer; got %v", layers)
		}
	}
}

func TestExplicitEnvironmentSelectsMatchingSelfConfigOverlay(t *testing.T) {
	dir := chdirToSelfConfig(t, `
default_environment: development
env_switcher: APP_ENV
sources:
  - type: yaml
    path: development.yaml
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.production.yaml"), []byte(`
sources:
  - type: yaml
    path: production.yaml
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "development.yaml"), []byte("selected: development\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "production.yaml"), []byte("selected: production\n"), 0o600))
	t.Setenv("APP_ENV", "development")

	cfg, err := confii.NewWithContext[any](context.Background(), confii.WithEnv("production"))
	require.NoError(t, err)
	value, err := cfg.Get("selected")
	require.NoError(t, err)
	assert.Equal(t, "production", value)
}

func TestLogLevel_ConstructsLogger(t *testing.T) {
	chdirToSelfConfig(t, "log_level: debug\n")

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	lg := cfg.Logger()
	require.NotNil(t, lg, ":self-config log_level must produce a non-nil *slog.Logger")
	assert.True(t, lg.Handler().Enabled(context.Background(), slog.LevelDebug),
		":log_level:debug must enable slog.LevelDebug on the constructed handler")

	assert.NotSame(t, slog.Default(), lg,
		":self-config log_level must construct a new logger, not return slog.Default")
}

func TestLogLevel_InvalidString_TypedError(t *testing.T) {
	chdirToSelfConfig(t, "log_level: bogus\n")

	_, err := confii.NewWithContext[any](context.Background())
	require.Error(t, err)

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		":invalid log_level must produce a typed *ConfigError; got %T (%v)", err, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad),
		":invalid log_level error must wrap ErrConfigLoad")
	assert.Contains(t, err.Error(), "bogus",
		":invalid log_level error must mention the offending value")
}

func TestLogLevel_ExplicitWithLogger_Wins(t *testing.T) {
	chdirToSelfConfig(t, "log_level: debug\n")

	var buf bytes.Buffer
	explicit := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLogger(explicit),
	)
	require.NoError(t, err)

	assert.Same(t, explicit, cfg.Logger(),
		":explicit WithLogger must beat self-config log_level")

	assert.False(t, cfg.Logger().Handler().Enabled(context.Background(), slog.LevelDebug),
		":explicit WithLogger preserves its own level (LevelError); debug must be filtered out")
}

func TestSources_AppendsToLoaders(t *testing.T) {
	dir := chdirToSelfConfig(t, `
sources:
  - type: yaml
    path: extra.yaml
  - type: json
    path: extra.json
  - type: environment
    prefix: APPCFG
`)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.yaml"),
		[]byte("yaml_only: from-yaml\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.json"),
		[]byte(`{"json_only": "from-json"}`), 0644))
	t.Setenv("APPCFG_ENV_ONLY", "from-env")

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	yamlVal, err := cfg.Get("yaml_only")
	require.NoError(t, err, ":sources[yaml] must be loaded")
	assert.Equal(t, "from-yaml", yamlVal)

	jsonVal, err := cfg.Get("json_only")
	require.NoError(t, err, ":sources[json] must be loaded")
	assert.Equal(t, "from-json", jsonVal)

	envVal, err := cfg.Get("env_only")
	require.NoError(t, err, "sources[environment, prefix=APPCFG] must be loaded")
	assert.Equal(t, "from-env", envVal)

	layers := cfg.Layers()
	sources := make(map[string]bool)
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok {
			sources[src] = true
		}
	}
	assert.True(t, sources["extra.yaml"], ":cfg.Layers must list extra.yaml; got %v", sources)
	assert.True(t, sources["extra.json"], ":cfg.Layers must list extra.json; got %v", sources)
	assert.True(t, sources["environment:APPCFG"], ":cfg.Layers must list environment:APPCFG; got %v", sources)
}

func TestSources_CanonicalTypesRetainConventionalExtensions(t *testing.T) {
	dir := chdirToSelfConfig(t, `
sources:
  - type: yaml
    path: config.yml
  - type: ini
    path: config.cfg
  - type: dotenv
    path: .env.local
`)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte("from_yaml: true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.cfg"), []byte("from_ini = true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.local"), []byte("FROM_DOTENV=true\n"), 0o600))

	cfg, err := confii.New[any]()
	require.NoError(t, err)
	for _, key := range []string{"from_yaml", "from_ini", "FROM_DOTENV"} {
		got, err := cfg.Get(key)
		require.NoError(t, err, key)
		assert.Equal(t, true, got, key)
	}
}

func TestSources_RejectsDeclaredTypeExtensionMismatch(t *testing.T) {
	chdirToSelfConfig(t, `
sources:
  - type: yaml
    path: config.json
`)

	_, err := confii.New[any]()
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigFormat)
	assert.Contains(t, err.Error(), "incompatible")
}

func TestSources_RejectsCrossTypeContent(t *testing.T) {
	for _, tt := range []struct {
		name     string
		typeName string
		path     string
		content  string
	}{
		{name: "JSON document declared as YAML", typeName: "yaml", path: "config.yaml", content: `{"server":{"port":8080}}`},
		{name: "YAML document declared as JSON", typeName: "json", path: "config.json", content: "server:\n  port:8080\n"},
		{name: "JSON document declared as TOML", typeName: "toml", path: "config.toml", content: `{"server":{"port":8080}}`},
		{name: "JSON document declared as INI", typeName: "ini", path: "config.ini", content: `{"server":{"port":8080}}`},
		{name: "JSON document declared as dotenv", typeName: "dotenv", path: "config.env", content: `{"server":{"port":8080}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := chdirToSelfConfig(t, fmt.Sprintf("sources:\n  - type: %s\n    path: %s\n", tt.typeName, tt.path))
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.path), []byte(tt.content), 0o600))

			_, err := confii.New[any]()
			require.Error(t, err)
		})
	}
}

func TestSelfConfigEnvPrefixOverridesDeclarativeSources(t *testing.T) {
	dir := chdirToSelfConfig(t, `
env_prefix: APP
sources:
  - type: yaml
    path: application.yaml
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "application.yaml"), []byte(`
server:
  port: 8080
`), 0644))
	t.Setenv("APP_SERVER__PORT", "9090")

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)
	port, err := cfg.Get("server.port")
	require.NoError(t, err)
	assert.Equal(t, 9090, port)

	layers := cfg.Layers()
	require.Len(t, layers, 2)
	assert.Equal(t, "application.yaml", layers[0]["source"])
	assert.Equal(t, "environment:APP", layers[1]["source"])
}

func TestMalformedSelfConfigFailsClosed(t *testing.T) {
	chdirToSelfConfig(t, "sources: [\n")

	_, err := confii.NewWithContext[any](context.Background())
	require.Error(t, err)
	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce))
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
	assert.Contains(t, err.Error(), "read self-config")
}

func TestSources_UnknownType_TypedError(t *testing.T) {
	chdirToSelfConfig(t, `
sources:
  - type: redis
    path: localhost:6379
`)

	_, err := confii.NewWithContext[any](context.Background())
	require.Error(t, err)

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		":unknown source type must produce a typed *ConfigError; got %T", err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad),
		":unknown source type must wrap ErrConfigLoad")
}

func TestSecrets_RegistersStores(t *testing.T) {
	dir := chdirToSelfConfig(t, `
sources:
  - type: yaml
    path: app.yaml
secrets:
  default_provider: local
  providers:
    local:
      type: env
      prefix: APP_
`)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yaml"),
		[]byte("db:\n  password: \"${secret:db/password}\"\n"), 0644))
	t.Setenv("APP_DB_PASSWORD", "s3cr3t")

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	pw, err := cfg.GetWithContext(context.Background(), "db.password")
	require.NoError(t, err,
		":${secret:db/password} must resolve via the self-config env secret hook")
	assert.Equal(t, "s3cr3t", pw)
}

func TestSecrets_UnknownProvider_TypedError(t *testing.T) {
	chdirToSelfConfig(t, `
secrets:
  providers:
    primary:
      type: bogusvault
`)

	_, err := confii.NewWithContext[any](context.Background())
	require.Error(t, err)

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce),
		":unknown secrets.provider must produce a typed *ConfigError; got %T", err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad),
		":unknown secrets.provider must wrap ErrConfigLoad")
	assert.Contains(t, err.Error(), "bogusvault")
}

func TestAllSettings_NoOpWhenAlreadyExplicit(t *testing.T) {
	dir := chdirToSelfConfig(t, `
env_prefix: APP
log_level: debug
sources:
  - type: yaml
    path: extra.yaml
secrets:
  default_provider: local
  providers:
    local:
      type: env
      prefix: SELF_
`)

	t.Setenv("WINNER_HOST", "from-explicit-prefix")
	t.Setenv("APP_HOST", "from-self-config-prefix")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.yaml"),
		[]byte("yaml_only: from-self-config-source\n"), 0644))

	var logBuf bytes.Buffer
	explicitLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))

	stub := &g04ValueLoader{src: "stub:explicit", data: map[string]any{
		"explicit_only": "from-explicit-loader",
		"db":            map[string]any{"password": "${secret:db/password}"},
	}}

	var hookCalls int
	explicitHook := hook.Func(func(_ context.Context, _ string, value any) (any, error) {
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

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(stub),
		confii.WithEnvPrefix("WINNER"),
		confii.WithLogger(explicitLogger),
		confii.WithSecretHook(explicitHook),
	)
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "from-explicit-prefix", host,
		":explicit WithEnvPrefix must beat self-config env_prefix")

	layers := cfg.Layers()
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok {
			assert.NotEqual(t, "environment:APP", src,
				":self-config env_prefix layer must not be installed when explicit WithEnvPrefix is supplied")
			assert.NotEqual(t, "extra.yaml", src,
				":self-config sources layer must not be installed when explicit WithLoaders is supplied")
		}
	}

	assert.Same(t, explicitLogger, cfg.Logger())
	assert.False(t, cfg.Logger().Handler().Enabled(context.Background(), slog.LevelDebug),
		":explicit logger preserves LevelError; self-config debug must NOT raise the level")

	exp, err := cfg.Get("explicit_only")
	require.NoError(t, err)
	assert.Equal(t, "from-explicit-loader", exp)
	_, err = cfg.Get("yaml_only")
	require.Error(t, err,
		":when WithLoaders is explicit, self-config sources must NOT also be loaded")

	pw, err := cfg.GetWithContext(context.Background(), "db.password")
	require.NoError(t, err,
		":explicit WithSecretHook must replace the self-config env-store hook")
	assert.Equal(t, "explicit-resolved", pw)
	assert.GreaterOrEqual(t, hookCalls, 1,
		":explicit secret hook must have been invoked at least once for the placeholder")
}

type g04ValueLoader struct {
	src  string
	data map[string]any
}

func (l *g04ValueLoader) Source() string { return l.src }
func (l *g04ValueLoader) Load(_ context.Context) (map[string]any, error) {
	return l.data, nil
}

var _ confii.Loader = (*g04ValueLoader)(nil)
