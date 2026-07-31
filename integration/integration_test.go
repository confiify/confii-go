// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/diff"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/confiify/confii-go/v2/merge"
	"github.com/confiify/confii-go/v2/observe"
	"github.com/confiify/confii-go/v2/secret"
	"github.com/confiify/confii-go/v2/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type AppConfig struct {
	App      App      `confii:"app"`
	Database Database `confii:"database"`
	Cache    Cache    `confii:"cache"`
	Features []string `confii:"features"`
}

type App struct {
	Name     string `confii:"name" validate:"required"`
	Version  string `confii:"version"`
	Debug    bool   `confii:"debug"`
	LogLevel string `confii:"log_level"`
}

type Database struct {
	Host           string `confii:"host" validate:"required"`
	Port           int    `confii:"port" validate:"required,min=1,max=65535"`
	Name           string `confii:"name" validate:"required"`
	MaxConnections int    `confii:"max_connections"`
	SSL            bool   `confii:"ssl"`
	PoolTimeout    int    `confii:"pool_timeout"`
}

type Cache struct {
	Enabled bool `confii:"enabled"`
	TTL     int  `confii:"ttl"`
}

type APIConfig struct {
	API     APISection     `confii:"api"`
	Logging LoggingSection `confii:"logging"`
}

type APISection struct {
	Host        string   `confii:"host" validate:"required"`
	Port        int      `confii:"port" validate:"required,min=1,max=65535"`
	CorsOrigins []string `confii:"cors_origins"`
}

type LoggingSection struct {
	Level  string `confii:"level"`
	Format string `confii:"format"`
}

func TestTypedConfig_ProductionEnvironment(t *testing.T) {
	cfg, err := confii.NewWithContext[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)

	assert.Equal(t, "my-service", model.App.Name)
	assert.Equal(t, "1.0.0", model.App.Version)
	assert.False(t, model.App.Debug)
	assert.Equal(t, "prod-db.example.com", model.Database.Host)
	assert.Equal(t, 5432, model.Database.Port)
	assert.Equal(t, "mydb", model.Database.Name)
	assert.Equal(t, 100, model.Database.MaxConnections)
	assert.True(t, model.Cache.Enabled)
	assert.Equal(t, 3600, model.Cache.TTL)
	assert.Equal(t, []string{"auth", "logging"}, model.Features)
}

func TestTypedConfig_StagingEnvironment(t *testing.T) {
	cfg, err := confii.NewWithContext[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("staging"),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)

	assert.True(t, model.App.Debug)
	assert.Equal(t, "staging-db.example.com", model.Database.Host)
	assert.Equal(t, 25, model.Database.MaxConnections)
	assert.Equal(t, 300, model.Cache.TTL)
}

func TestMultipleLoaders_DeepMerge(t *testing.T) {
	cfg, err := confii.NewWithContext[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml"),
			loader.NewYAML("testdata/overrides.yaml"),
		),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)

	assert.Equal(t, "prod-db.example.com", model.Database.Host)
	assert.Equal(t, 100, model.Database.MaxConnections)

	assert.True(t, model.Database.SSL)
	assert.Equal(t, 30, model.Database.PoolTimeout)
	assert.Equal(t, "warn", model.App.LogLevel)
}

func TestMixedFormats(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json"),
			loader.NewTOML("testdata/app.toml"),
			loader.NewINI("testdata/legacy.ini"),
			loader.NewEnvFile("testdata/secrets.env"),
		),
	)
	require.NoError(t, err)

	host, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", host)

	readTimeout, err := cfg.GetInt("server.read_timeout")
	require.NoError(t, err)
	assert.Equal(t, 30, readTimeout)

	metricsEnabled, err := cfg.GetBool("metrics.enabled")
	require.NoError(t, err)
	assert.True(t, metricsEnabled)

	smtpHost, err := cfg.GetString("smtp.host")
	require.NoError(t, err)
	assert.Equal(t, "mail.example.com", smtpHost)

	smtpTLS, err := cfg.GetBool("smtp.use_tls")
	require.NoError(t, err)
	assert.True(t, smtpTLS)

	dbPass, err := cfg.GetString("DATABASE_PASSWORD")
	require.NoError(t, err)
	assert.Equal(t, "s3cret_from_env", dbPass)

	redisURL, err := cfg.GetString("REDIS_URL")
	require.NoError(t, err)
	assert.Equal(t, "redis://localhost:6379/0", redisURL)

	keys := cfg.Keys()
	assert.Contains(t, keys, "api.host")
	assert.Contains(t, keys, "server.port")
	assert.Contains(t, keys, "smtp.host")
	assert.Contains(t, keys, "DATABASE_PASSWORD")
}

func TestEnvVarOverride(t *testing.T) {
	t.Setenv("TESTAPP_HOST", "env-override-host")
	t.Setenv("TESTAPP_PORT", "9999")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json"),
			loader.NewEnvironment("TESTAPP"),
		),
	)
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "env-override-host", host)

	port, err := cfg.GetInt("port")
	require.NoError(t, err)
	assert.Equal(t, 9999, port)

	apiHost, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", apiHost)
}

func TestEnvSwitcher(t *testing.T) {
	t.Setenv("APP_ENVIRONMENT", "staging")

	cfg, err := confii.NewWithContext[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnvSwitcher("APP_ENVIRONMENT"),
	)
	require.NoError(t, err)

	assert.Equal(t, "staging", cfg.Env())

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "staging-db.example.com", model.Database.Host)
}

func TestSysenvFallback(t *testing.T) {
	t.Setenv("EXTERNAL_API_URL", "https://api.example.com")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithSysenvFallback(true),
	)
	require.NoError(t, err)

	url, err := cfg.Get("external.api.url")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", url)
}

func TestSysenvFallback_WithPrefix(t *testing.T) {
	t.Setenv("MYAPP_REDIS_HOST", "redis.local")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithSysenvFallback(true),
		confii.WithEnvPrefix("MYAPP"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("redis.host")
	require.NoError(t, err)
	assert.Equal(t, "redis.local", host)
}

func TestEnvVarExpansion(t *testing.T) {
	t.Setenv("DB_HOST_FROM_ENV", "expanded-host")
	t.Setenv("DB_PORT_FROM_ENV", "6543")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvExpander(true),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("database.host", "${DB_HOST_FROM_ENV}"))
	require.NoError(t, cfg.Set("database.url", "postgres://${DB_HOST_FROM_ENV}:${DB_PORT_FROM_ENV}/mydb"))

	host, _ := cfg.Get("database.host")
	assert.Equal(t, "expanded-host", host)

	url, _ := cfg.Get("database.url")
	assert.Equal(t, "postgres://expanded-host:6543/mydb", url)
}

func TestSecretResolution_EndToEnd(t *testing.T) {

	store := secret.NewDictStore(map[string]any{
		"db/password":    "super-secret-pw",
		"api/key":        "key-12345",
		"db/full_config": map[string]any{"host": "secret-host", "port": 5432},
	})

	resolver := secret.NewResolver(store,
		secret.WithCache(true),
		secret.WithCacheTTL(1*time.Minute),
	)

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithTypeCasting(false),
		confii.WithGlobalHook(resolver.Hook()),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("database.password", "${secret:db/password}"))
	require.NoError(t, cfg.Set("api.key", "${secret:api/key}"))
	require.NoError(t, cfg.Set("database.secret_host", "${secret:db/full_config:host}"))

	pw, _ := cfg.Get("database.password")
	assert.Equal(t, "super-secret-pw", pw)

	key, _ := cfg.Get("api.key")
	assert.Equal(t, "key-12345", key)

	secretHost, _ := cfg.Get("database.secret_host")
	assert.Equal(t, "secret-host", secretHost)

	stats := resolver.CacheStats()
	assert.Equal(t, true, stats["enabled"])
	assert.Greater(t, stats["size"], 0)
}

func TestSecretResolution_Hook_FailOnMissing_PropagatesError(t *testing.T) {
	store := secret.NewDictStore(map[string]any{"db/password": "ok"})

	resolver := secret.NewResolver(store, secret.WithCache(false))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithTypeCasting(false),
		confii.WithGlobalHook(resolver.Hook()),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("database.password", "${secret:db/password}"))
	got, err := cfg.GetWithContext(context.Background(), "database.password")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)

	err = cfg.Set("database.missing", "${secret:does/not/exist}")
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrSecretNotFound)
	assert.False(t, cfg.Has("database.missing"))
}

func TestJSONSchemaValidation(t *testing.T) {
	cfg, err := confii.NewWithContext[APIConfig](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
	)
	require.NoError(t, err)

	v, err := validate.NewJSONSchemaValidatorFromFile("testdata/schema.json")
	require.NoError(t, err)

	data, err := cfg.ToDict()
	require.NoError(t, err)
	err = v.Validate(data)
	assert.NoError(t, err)
}

func TestJSONSchemaValidation_Failure(t *testing.T) {

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewTOML("testdata/app.toml")),
	)
	require.NoError(t, err)

	v, err := validate.NewJSONSchemaValidatorFromFile("testdata/schema.json")
	require.NoError(t, err)

	data, err := cfg.ToDict()
	require.NoError(t, err)
	err = v.Validate(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestStructTagValidation(t *testing.T) {
	cfg, err := confii.NewWithContext[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "my-service", model.App.Name)
}

func TestStructTagValidation_MissingRequired(t *testing.T) {

	cfg, err := confii.NewWithContext[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewTOML("testdata/app.toml")),
	)
	require.NoError(t, err)

	_, err = cfg.Typed()
	assert.Error(t, err)
}

func TestBuilderPattern(t *testing.T) {
	cfg, err := confii.NewBuilder[AppConfig]().
		WithEnv("staging").
		AddLoader(loader.NewYAML("testdata/base.yaml")).
		AddLoader(loader.NewYAML("testdata/overrides.yaml")).
		WithMergeStrategy(confii.StrategyMerge).
		BuildWithContext(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.Env())

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "staging-db.example.com", model.Database.Host)
	assert.True(t, model.Database.SSL)
}

func TestFreeze(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())

	err = cfg.Set("database.host", "hacked")
	assert.True(t, errors.Is(err, confii.ErrConfigFrozen))

	err = cfg.ReloadWithContext(context.Background())
	assert.True(t, errors.Is(err, confii.ErrConfigFrozen))

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)
}

func TestOverrideAndRestore(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	original, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", original)

	restore, err := cfg.Override(map[string]any{
		"database.host": "test-db",
		"database.port": 1111,
	})
	require.NoError(t, err)

	overridden, _ := cfg.Get("database.host")
	assert.Equal(t, "test-db", overridden)

	port, _ := cfg.GetInt("database.port")
	assert.Equal(t, 1111, port)

	restore()

	restored, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", restored)

	restoredPort, _ := cfg.GetInt("database.port")
	assert.Equal(t, 5432, restoredPort)
}

func TestReload(t *testing.T) {

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("host: original\nport: 1234"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
	)
	require.NoError(t, err)

	host, _ := cfg.Get("host")
	assert.Equal(t, "original", host)

	require.NoError(t, os.WriteFile(cfgPath, []byte("host: updated\nport: 5678"), 0644))

	require.NoError(t, cfg.ReloadWithContext(context.Background()))

	host, _ = cfg.Get("host")
	assert.Equal(t, "updated", host)

	port, _ := cfg.GetInt("port")
	assert.Equal(t, 5678, port)
}

func TestOnChangeCallback(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("host: before"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
	)
	require.NoError(t, err)

	var changes []string
	cfg.OnChange(func(key string, oldVal, newVal any) {
		changes = append(changes, key)
	})

	require.NoError(t, os.WriteFile(cfgPath, []byte("host: after"), 0644))
	require.NoError(t, cfg.ReloadWithContext(context.Background()))

	assert.Contains(t, changes, "host")
}

func TestExport(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
	)
	require.NoError(t, err)

	jsonData, err := cfg.Export("json")
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"host"`)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(jsonData, &parsed))
	assert.Contains(t, parsed, "api")

	yamlData, err := cfg.Export("yaml")
	require.NoError(t, err)
	assert.Contains(t, string(yamlData), "host:")
}

func newIntegrationHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("loopback bind unavailable (hermetic environment): %v", r)
		}
	}()
	return httptest.NewServer(handler)
}

func TestHTTPLoader_RealServer(t *testing.T) {
	srv := newIntegrationHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"remote": map[string]any{
				"setting": "from-http",
				"count":   42,
			},
		})
	}))
	defer srv.Close()

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json"),
			loader.NewHTTP(srv.URL),
		),
	)
	require.NoError(t, err)

	setting, err := cfg.Get("remote.setting")
	require.NoError(t, err)
	assert.Equal(t, "from-http", setting)

	apiHost, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", apiHost)
}

func TestDiffEnvironments(t *testing.T) {
	devCfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("staging"),
	)
	require.NoError(t, err)

	prodCfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	devData, err := devCfg.ToDict()
	require.NoError(t, err)
	prodData, err := prodCfg.ToDict()
	require.NoError(t, err)
	diffs := diff.Diff(devData, prodData)
	assert.NotEmpty(t, diffs)

	summary := diff.Summary(diffs)
	assert.Greater(t, summary["modified"], 0)

	jsonStr, err := diff.ToJSON(diffs)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, "modified")
}

func TestDriftDetection(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	baseline, err := cfg.ToDict()
	require.NoError(t, err)

	drifted := make(map[string]any)
	for k, v := range baseline {
		drifted[k] = v
	}
	drifted["database"] = map[string]any{
		"host":            "rogue-db.example.com",
		"port":            5432,
		"name":            "mydb",
		"max_connections": 100,
	}

	detector := diff.NewDriftDetector(baseline)
	assert.True(t, detector.HasDrift(drifted))

	driftDiffs := detector.DetectDrift(drifted)
	assert.NotEmpty(t, driftDiffs)
}

func TestObservability(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	metrics := observe.NewMetrics(len(cfg.Keys()))

	start := time.Now()
	_, _ = cfg.Get("database.host")
	metrics.RecordAccess("database.host", time.Since(start))

	_, _ = cfg.Get("database.port")
	metrics.RecordAccess("database.port", time.Since(start))

	stats := metrics.Statistics()
	assert.Equal(t, 2, stats["accessed_keys"])

	emitter := observe.NewEventEmitter(nil)
	var reloadCount int
	emitter.On("reload", func(_ ...any) { reloadCount++ })

	data, err := cfg.ToDict()
	require.NoError(t, err)
	emitter.Emit("reload", data)
	assert.Equal(t, 1, reloadCount)
}

func TestVersioning(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	dir := t.TempDir()
	vm := observe.NewVersionManager(dir, 100)

	data, err := cfg.ToDict()
	require.NoError(t, err)
	v1, err := vm.SaveVersion(data, map[string]any{"author": "deploy-bot", "env": "production"})
	require.NoError(t, err)
	assert.NotEmpty(t, v1.VersionID)

	require.NoError(t, cfg.Set("database.host", "new-host"))

	data, err = cfg.ToDict()
	require.NoError(t, err)
	v2, err := vm.SaveVersion(data, map[string]any{"author": "deploy-bot"})
	require.NoError(t, err)
	assert.NotEqual(t, v1.VersionID, v2.VersionID)

	versions := vm.ListVersions()
	assert.Len(t, versions, 2)

	retrieved := vm.GetVersion(v1.VersionID)
	require.NotNil(t, retrieved)
	db := retrieved.Config["database"].(map[string]any)
	assert.Equal(t, "prod-db.example.com", db["host"])
}

func TestAdvancedMergeStrategies(t *testing.T) {
	base := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432},
		"features": []any{"auth", "logging"},
		"cache":    map[string]any{"ttl": 300, "enabled": true},
	}
	overlay := map[string]any{
		"database": map[string]any{"host": "prod-db"},
		"features": []any{"metrics"},
		"cache":    map[string]any{"ttl": 3600},
	}

	m := merge.NewAdvanced(merge.DeepMergeStrategy, map[string]merge.Strategy{
		"database": merge.Replace,
		"features": merge.Append,
	})

	result := m.Merge(base, overlay)

	db := result["database"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	_, hasPort := db["port"]
	assert.False(t, hasPort)

	assert.Equal(t, []any{"auth", "logging", "metrics"}, result["features"])

	cache := result["cache"].(map[string]any)
	assert.Equal(t, 3600, cache["ttl"])
	assert.Equal(t, true, cache["enabled"])
}

func TestMultiSecretStore_FallbackChain(t *testing.T) {

	primary := secret.NewDictStore(map[string]any{
		"db/password": "primary-pw",
	})
	secondary := secret.NewDictStore(map[string]any{
		"api/key":     "secondary-key",
		"db/password": "secondary-pw",
	})

	multi := secret.NewMultiStore([]confii.SecretStore{primary, secondary})
	ctx := context.Background()

	pw, err := multi.GetSecret(ctx, "db/password")
	require.NoError(t, err)
	assert.Equal(t, "primary-pw", pw)

	key, err := multi.GetSecret(ctx, "api/key")
	require.NoError(t, err)
	assert.Equal(t, "secondary-key", key)

	_, err = multi.GetSecret(ctx, "missing")
	assert.Error(t, err)
}

func TestSecretResolver_PrefixAndCacheTTL(t *testing.T) {
	store := secret.NewDictStore(map[string]any{
		"prod/db/password": "versioned-pw",
	})

	resolver := secret.NewResolver(store,
		secret.WithResolverPrefix("prod/"),
		secret.WithCache(true),
		secret.WithCacheTTL(50*time.Millisecond),
	)

	ctx := context.Background()

	val, err := resolver.Resolve(ctx, "${secret:db/password}")
	require.NoError(t, err)
	assert.Equal(t, "versioned-pw", val)

	_ = store.SetSecret(ctx, "prod/db/password", "new-pw")

	val, _ = resolver.Resolve(ctx, "${secret:db/password}")
	assert.Equal(t, "versioned-pw", val)

	time.Sleep(60 * time.Millisecond)

	val, _ = resolver.Resolve(ctx, "${secret:db/password}")
	assert.Equal(t, "new-pw", val)
}

func TestConcurrentAccess(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := cfg.Get("database.host"); err != nil {
					errs <- err
				}
				cfg.Keys()
				cfg.Has("database.port")
				if _, err := cfg.ToDict(); err != nil {
					errs <- err
				}
				cfg.GetStringOr("app.name", "default")
				cfg.GetIntOr("database.port", 0)
				cfg.GetBoolOr("app.debug", false)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent read error: %v", err)
	}
}

func TestNotFoundError(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	_, err = cfg.Get("database.hst")
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigNotFound))
}

func TestGetOrConvenience(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	assert.Equal(t, "prod-db.example.com", cfg.GetOr("database.host", "fallback"))
	assert.Equal(t, "fallback", cfg.GetOr("nonexistent.key", "fallback"))
	assert.Equal(t, "default-val", cfg.GetStringOr("missing", "default-val"))
	assert.Equal(t, 42, cfg.GetIntOr("missing.int", 42))
	assert.True(t, cfg.GetBoolOr("missing.bool", true))
}

func TestFullPipeline(t *testing.T) {
	t.Setenv("PIPELINE_APP_VERSION", "2.0.0")

	store := secret.NewDictStore(map[string]any{
		"db_password": "pipeline-secret",
	})
	resolver := secret.NewResolver(store)

	cfg, err := confii.NewBuilder[any]().
		WithEnv("production").
		AddLoader(loader.NewYAML("testdata/base.yaml")).
		AddLoader(loader.NewYAML("testdata/overrides.yaml")).
		WithMergeStrategy(confii.StrategyMerge).
		WithGlobalHook(resolver.Hook()).
		BuildWithContext(context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("database.password", "${secret:db_password}"))
	require.NoError(t, cfg.Set("app.computed_version", "${PIPELINE_APP_VERSION}"))

	pw, _ := cfg.Get("database.password")
	assert.Equal(t, "pipeline-secret", pw)

	ver, _ := cfg.Get("app.computed_version")
	assert.Equal(t, "2.0.0", ver)

	host, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", host)

	ssl, _ := cfg.GetBool("database.ssl")
	assert.True(t, ssl)

	logLevel, _ := cfg.Get("app.log_level")
	assert.Equal(t, "warn", logLevel)
}

func TestComposition_IncludeAndDefaults(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/with_include.yaml")),
	)
	require.NoError(t, err)

	logLevel, err := cfg.Get("shared.log_level")
	require.NoError(t, err)
	assert.Equal(t, "info", logLevel)

	retries, err := cfg.GetInt("shared.max_retries")
	require.NoError(t, err)
	assert.Equal(t, 3, retries)

	timeout, err := cfg.Get("timeout")
	require.NoError(t, err)
	assert.Equal(t, 30, timeout)

	name, err := cfg.Get("app.name")
	require.NoError(t, err)
	assert.Equal(t, "composed-app", name)

	assert.False(t, cfg.Has("_include"))
	assert.False(t, cfg.Has("_defaults"))
}

func TestSourceTracking(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml"),
			loader.NewYAML("testdata/overrides.yaml"),
		),
		confii.WithEnv("production"),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	info := cfg.GetSourceInfo("database.host")
	assert.NotNil(t, info)
	assert.Equal(t, "prod-db.example.com", info.Value)

	stats := cfg.GetSourceStatistics()
	assert.Greater(t, stats["total_keys"], 0)

	keys := cfg.FindKeysFromSource("base.yaml")
	assert.NotEmpty(t, keys)

	debugOutput := cfg.PrintDebugInfo("database.host")
	assert.Contains(t, debugOutput, "database.host")
	assert.Contains(t, debugOutput, "prod-db.example.com")

	conflicts := cfg.GetConflicts()

	assert.NotNil(t, conflicts)
}

func TestExplain(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	explanation := cfg.Explain("database.host")
	assert.Equal(t, true, explanation["exists"])
	assert.Equal(t, "prod-db.example.com", explanation["current_value"])
	assert.Equal(t, "production", explanation["environment"])

	missing := cfg.Explain("nonexistent.key")
	assert.Equal(t, false, missing["exists"])
	assert.NotNil(t, missing["available_keys"])
}

func TestSchemaInfo(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	info := cfg.Schema("database.host")
	assert.Equal(t, true, info["exists"])
	assert.Equal(t, "string", info["type"])
	assert.Equal(t, "prod-db.example.com", info["value"])
}

func TestLayers(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml"),
			loader.NewYAML("testdata/overrides.yaml"),
		),
		confii.WithEnv("production"),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	layers := cfg.Layers()
	assert.Len(t, layers, 2)
	assert.Contains(t, layers[0]["source"], "base.yaml")
	assert.Contains(t, layers[1]["source"], "overrides.yaml")
	assert.Greater(t, layers[0]["key_count"], 0)
}

func TestGenerateDocs(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	md, err := cfg.GenerateDocs("markdown")
	require.NoError(t, err)
	assert.Contains(t, md, "api.host")
	assert.Contains(t, md, "Key")

	jsonDocs, err := cfg.GenerateDocs("json")
	require.NoError(t, err)
	assert.Contains(t, jsonDocs, "api.host")
}

func TestSet_OverrideFalse(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	err = cfg.Set("database.host", "new-host", confii.WithOverride(false))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	err = cfg.Set("new.key", "value", confii.WithOverride(false))
	assert.NoError(t, err)
}

func TestExtend(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	assert.False(t, cfg.Has("api.host"))

	err = cfg.ExtendWithContext(context.Background(), loader.NewJSON("testdata/flat.json"))
	require.NoError(t, err)

	apiHost, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", apiHost)

	dbHost, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", dbHost)
}

func TestExport_TOML(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
	)
	require.NoError(t, err)

	data, err := cfg.Export("toml")
	require.NoError(t, err)
	assert.Contains(t, string(data), "host")
}

func TestExport_ToFile(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
	)
	require.NoError(t, err)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "exported.json")

	_, err = cfg.Export("json", outPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "api")
}

func TestReload_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("host: original"), 0644)

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
	)
	require.NoError(t, err)

	_ = os.WriteFile(cfgPath, []byte("host: modified"), 0644)

	err = cfg.ReloadWithContext(context.Background(), confii.WithDryRun(true), confii.WithIncremental(false))
	require.NoError(t, err)

	host, _ := cfg.Get("host")
	assert.Equal(t, "original", host)
}

func TestObservability_IntegratedOnConfig(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	metrics := cfg.EnableObservability()
	assert.NotNil(t, metrics)

	emitter := cfg.EnableEvents()
	assert.NotNil(t, emitter)

	emitter.On("reload", func(_ ...any) {})

	m := cfg.GetMetrics()
	assert.NotNil(t, m)
}

func TestVersioning_IntegratedOnConfig(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	dir := t.TempDir()
	cfg.EnableVersioning(dir, 100)

	v1, err := cfg.SaveVersion(map[string]any{"author": "test"})
	require.NoError(t, err)
	assert.NotEmpty(t, v1.VersionID)

	_ = cfg.Set("database.host", "rollback-target")

	v2, err := cfg.SaveVersion(nil)
	require.NoError(t, err)

	err = cfg.RollbackToVersion(v1.VersionID)
	require.NoError(t, err)

	host, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", host)

	_ = v2
}

func TestDiff_OnConfig(t *testing.T) {
	cfg1, _ := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("staging"),
	)
	cfg2, _ := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)

	diffs, err := cfg1.Diff(cfg2)
	require.NoError(t, err)
	assert.NotEmpty(t, diffs)
}

func TestDetectDrift_OnConfig(t *testing.T) {
	cfg, _ := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)

	intended := map[string]any{
		"database": map[string]any{"host": "expected-host", "port": 5432},
	}

	drifts, err := cfg.DetectDrift(intended)
	require.NoError(t, err)
	assert.NotEmpty(t, drifts)
}

func TestStopWatching(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	cfg.StopWatching()
}

func TestExportDebugReport(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	dir := t.TempDir()
	reportPath := filepath.Join(dir, "debug_report.json")

	err = cfg.ExportDebugReport(reportPath)
	require.NoError(t, err)

	data, err := os.ReadFile(reportPath)
	require.NoError(t, err)

	var report map[string]any
	require.NoError(t, json.Unmarshal(data, &report))
	assert.NotEmpty(t, report)
}
