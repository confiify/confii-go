// Package integration contains end-to-end tests that exercise confii
// the way a real consumer would: import the library, load real config files,
// and verify the full pipeline works. No mocks or stubs.
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

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/diff"
	"github.com/confiify/confii-go/loader"
	"github.com/confiify/confii-go/loader/cloud"
	"github.com/confiify/confii-go/merge"
	"github.com/confiify/confii-go/observe"
	"github.com/confiify/confii-go/secret"
	"github.com/confiify/confii-go/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Typed config structs — what a real app would define
// ---------------------------------------------------------------------------

type AppConfig struct {
	App      App      `mapstructure:"app"`
	Database Database `mapstructure:"database"`
	Cache    Cache    `mapstructure:"cache"`
	Features []string `mapstructure:"features"`
}

type App struct {
	Name     string `mapstructure:"name" validate:"required"`
	Version  string `mapstructure:"version"`
	Debug    bool   `mapstructure:"debug"`
	LogLevel string `mapstructure:"log_level"`
}

type Database struct {
	Host           string `mapstructure:"host" validate:"required"`
	Port           int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Name           string `mapstructure:"name" validate:"required"`
	MaxConnections int    `mapstructure:"max_connections"`
	SSL            bool   `mapstructure:"ssl"`
	PoolTimeout    int    `mapstructure:"pool_timeout"`
}

type Cache struct {
	Enabled bool `mapstructure:"enabled"`
	TTL     int  `mapstructure:"ttl"`
}

type APIConfig struct {
	API     APISection     `mapstructure:"api"`
	Logging LoggingSection `mapstructure:"logging"`
}

type APISection struct {
	Host        string   `mapstructure:"host" validate:"required"`
	Port        int      `mapstructure:"port" validate:"required,min=1,max=65535"`
	CorsOrigins []string `mapstructure:"cors_origins"`
}

type LoggingSection struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// ---------------------------------------------------------------------------
// Test: Load a YAML file, resolve environment, access with typed model
// ---------------------------------------------------------------------------

func TestTypedConfig_ProductionEnvironment(t *testing.T) {
	cfg, err := confii.New[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	// Verify environment resolution merged default + production.
	model, err := cfg.Typed()
	require.NoError(t, err)

	assert.Equal(t, "my-service", model.App.Name)
	assert.Equal(t, "1.0.0", model.App.Version)
	assert.False(t, model.App.Debug)                            // production overrides default
	assert.Equal(t, "prod-db.example.com", model.Database.Host) // production
	assert.Equal(t, 5432, model.Database.Port)                  // from default
	assert.Equal(t, "mydb", model.Database.Name)                // from default
	assert.Equal(t, 100, model.Database.MaxConnections)         // production
	assert.True(t, model.Cache.Enabled)                         // from default
	assert.Equal(t, 3600, model.Cache.TTL)                      // production
	assert.Equal(t, []string{"auth", "logging"}, model.Features)
}

func TestTypedConfig_StagingEnvironment(t *testing.T) {
	cfg, err := confii.New[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("staging"),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)

	assert.True(t, model.App.Debug)                                // staging
	assert.Equal(t, "staging-db.example.com", model.Database.Host) // staging
	assert.Equal(t, 25, model.Database.MaxConnections)             // staging
	assert.Equal(t, 300, model.Cache.TTL)                          // from default (staging doesn't override)
}

// ---------------------------------------------------------------------------
// Test: Multiple loaders, deep merge across YAML files
// ---------------------------------------------------------------------------

func TestMultipleLoaders_DeepMerge(t *testing.T) {
	cfg, err := confii.New[AppConfig](context.Background(),
		confii.WithLoaders(
			loader.NewYAML("testdata/base.yaml"),
			loader.NewYAML("testdata/overrides.yaml"),
		),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)

	// From base.yaml production section.
	assert.Equal(t, "prod-db.example.com", model.Database.Host)
	assert.Equal(t, 100, model.Database.MaxConnections)

	// From overrides.yaml production section (deep merged in).
	assert.True(t, model.Database.SSL)
	assert.Equal(t, 30, model.Database.PoolTimeout)
	assert.Equal(t, "warn", model.App.LogLevel)
}

// ---------------------------------------------------------------------------
// Test: Mix formats — YAML + JSON + TOML + INI + .env in one Config
// ---------------------------------------------------------------------------

func TestMixedFormats(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewJSON("testdata/flat.json"),
			loader.NewTOML("testdata/app.toml"),
			loader.NewINI("testdata/legacy.ini"),
			loader.NewEnvFile("testdata/secrets.env"),
		),
	)
	require.NoError(t, err)

	// From JSON.
	host, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", host)

	// From TOML.
	readTimeout, err := cfg.GetInt("server.read_timeout")
	require.NoError(t, err)
	assert.Equal(t, 30, readTimeout)

	metricsEnabled, err := cfg.GetBool("metrics.enabled")
	require.NoError(t, err)
	assert.True(t, metricsEnabled)

	// From INI.
	smtpHost, err := cfg.GetString("smtp.host")
	require.NoError(t, err)
	assert.Equal(t, "mail.example.com", smtpHost)

	smtpTLS, err := cfg.GetBool("smtp.use_tls")
	require.NoError(t, err)
	assert.True(t, smtpTLS)

	// From .env.
	dbPass, err := cfg.GetString("DATABASE_PASSWORD")
	require.NoError(t, err)
	assert.Equal(t, "s3cret_from_env", dbPass)

	redisURL, err := cfg.GetString("REDIS_URL")
	require.NoError(t, err)
	assert.Equal(t, "redis://localhost:6379/0", redisURL)

	// Verify all keys from all sources are present.
	keys := cfg.Keys()
	assert.Contains(t, keys, "api.host")
	assert.Contains(t, keys, "server.port")
	assert.Contains(t, keys, "smtp.host")
	assert.Contains(t, keys, "DATABASE_PASSWORD")
}

// ---------------------------------------------------------------------------
// Test: Environment variables override file config
// ---------------------------------------------------------------------------

func TestEnvVarOverride(t *testing.T) {
	t.Setenv("TESTAPP_HOST", "env-override-host")
	t.Setenv("TESTAPP_PORT", "9999")

	// Use a flat config (no environment sections) so env loader merges cleanly.
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewJSON("testdata/flat.json"),
			loader.NewEnvironment("TESTAPP"),
		),
	)
	require.NoError(t, err)

	// Env loader runs after JSON, so it overrides.
	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "env-override-host", host)

	port, err := cfg.GetInt("port")
	require.NoError(t, err)
	assert.Equal(t, 9999, port)

	// Non-overridden values still come from file.
	apiHost, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", apiHost)
}

// ---------------------------------------------------------------------------
// Test: EnvSwitcher reads environment from OS variable
// ---------------------------------------------------------------------------

func TestEnvSwitcher(t *testing.T) {
	t.Setenv("APP_ENVIRONMENT", "staging")

	cfg, err := confii.New[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnvSwitcher("APP_ENVIRONMENT"),
	)
	require.NoError(t, err)

	assert.Equal(t, "staging", cfg.Env())

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "staging-db.example.com", model.Database.Host)
}

// ---------------------------------------------------------------------------
// Test: SysenvFallback — missing keys resolved from OS environment
// ---------------------------------------------------------------------------

func TestSysenvFallback(t *testing.T) {
	t.Setenv("EXTERNAL_API_URL", "https://api.example.com")

	cfg, err := confii.New[any](context.Background(),
		confii.WithSysenvFallback(true),
	)
	require.NoError(t, err)

	url, err := cfg.Get("external.api.url")
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", url)
}

func TestSysenvFallback_WithPrefix(t *testing.T) {
	t.Setenv("MYAPP_REDIS_HOST", "redis.local")

	cfg, err := confii.New[any](context.Background(),
		confii.WithSysenvFallback(true),
		confii.WithEnvPrefix("MYAPP"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("redis.host")
	require.NoError(t, err)
	assert.Equal(t, "redis.local", host)
}

// ---------------------------------------------------------------------------
// Test: ${VAR} expansion in config values
// ---------------------------------------------------------------------------

func TestEnvVarExpansion(t *testing.T) {
	t.Setenv("DB_HOST_FROM_ENV", "expanded-host")
	t.Setenv("DB_PORT_FROM_ENV", "6543")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvExpander(true),
	)
	require.NoError(t, err)

	// Set values with placeholders.
	require.NoError(t, cfg.Set("database.host", "${DB_HOST_FROM_ENV}"))
	require.NoError(t, cfg.Set("database.url", "postgres://${DB_HOST_FROM_ENV}:${DB_PORT_FROM_ENV}/mydb"))

	host, _ := cfg.Get("database.host")
	assert.Equal(t, "expanded-host", host)

	url, _ := cfg.Get("database.url")
	assert.Equal(t, "postgres://expanded-host:6543/mydb", url)
}

// ---------------------------------------------------------------------------
// Test: Secret resolution end-to-end (DictStore → Resolver → Hook → Config)
// ---------------------------------------------------------------------------

func TestSecretResolution_EndToEnd(t *testing.T) {
	// Simulate a real secret store with application secrets.
	store := secret.NewDictStore(map[string]any{
		"db/password":    "super-secret-pw",
		"api/key":        "key-12345",
		"db/full_config": map[string]any{"host": "secret-host", "port": 5432},
	})

	resolver := secret.NewResolver(store,
		secret.WithCache(true),
		secret.WithCacheTTL(1*time.Minute),
	)

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithTypeCasting(false), // disable so resolved strings aren't re-cast
	)
	require.NoError(t, err)

	// Register the secret resolver hook.
	cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())

	// Set values with secret placeholders.
	require.NoError(t, cfg.Set("database.password", "${secret:db/password}"))
	require.NoError(t, cfg.Set("api.key", "${secret:api/key}"))
	require.NoError(t, cfg.Set("database.secret_host", "${secret:db/full_config:host}"))

	// Verify secrets are resolved on access.
	pw, _ := cfg.Get("database.password")
	assert.Equal(t, "super-secret-pw", pw)

	key, _ := cfg.Get("api.key")
	assert.Equal(t, "key-12345", key)

	// JSON path extraction.
	secretHost, _ := cfg.Get("database.secret_host")
	assert.Equal(t, "secret-host", secretHost)

	// Verify caching.
	stats := resolver.CacheStats()
	assert.Equal(t, true, stats["enabled"])
	assert.Greater(t, stats["size"], 0)
}

// ---------------------------------------------------------------------------
// Test: JSON Schema validation with real schema file
// ---------------------------------------------------------------------------

func TestJSONSchemaValidation(t *testing.T) {
	cfg, err := confii.New[APIConfig](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
	)
	require.NoError(t, err)

	// Validate against schema file.
	v, err := validate.NewJSONSchemaValidatorFromFile("testdata/schema.json")
	require.NoError(t, err)

	err = v.Validate(cfg.ToDict())
	assert.NoError(t, err)
}

func TestJSONSchemaValidation_Failure(t *testing.T) {
	// Config missing required "api" key.
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewTOML("testdata/app.toml")),
	)
	require.NoError(t, err)

	v, err := validate.NewJSONSchemaValidatorFromFile("testdata/schema.json")
	require.NoError(t, err)

	err = v.Validate(cfg.ToDict())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// ---------------------------------------------------------------------------
// Test: Struct tag validation via Typed()
// ---------------------------------------------------------------------------

func TestStructTagValidation(t *testing.T) {
	cfg, err := confii.New[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "my-service", model.App.Name)
}

func TestStructTagValidation_MissingRequired(t *testing.T) {
	// Load a config that won't have the required fields for AppConfig.
	cfg, err := confii.New[AppConfig](context.Background(),
		confii.WithLoaders(loader.NewTOML("testdata/app.toml")),
	)
	require.NoError(t, err)

	_, err = cfg.Typed()
	assert.Error(t, err) // validation should fail — no app.name, database.host etc.
}

// ---------------------------------------------------------------------------
// Test: Builder pattern
// ---------------------------------------------------------------------------

func TestBuilderPattern(t *testing.T) {
	cfg, err := confii.NewBuilder[AppConfig]().
		WithEnv("staging").
		AddLoader(loader.NewYAML("testdata/base.yaml")).
		AddLoader(loader.NewYAML("testdata/overrides.yaml")).
		EnableDeepMerge().
		Build(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.Env())

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "staging-db.example.com", model.Database.Host)
	assert.True(t, model.Database.SSL) // from overrides
}

// ---------------------------------------------------------------------------
// Test: Freeze prevents mutation
// ---------------------------------------------------------------------------

func TestFreeze(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())

	// Set should fail.
	err = cfg.Set("database.host", "hacked")
	assert.True(t, errors.Is(err, confii.ErrConfigFrozen))

	// Reload should fail.
	err = cfg.Reload(context.Background())
	assert.True(t, errors.Is(err, confii.ErrConfigFrozen))

	// Reads still work.
	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)
}

// ---------------------------------------------------------------------------
// Test: Override + restore
// ---------------------------------------------------------------------------

func TestOverrideAndRestore(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	original, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", original)

	// Override.
	restore, err := cfg.Override(map[string]any{
		"database.host": "test-db",
		"database.port": 1111,
	})
	require.NoError(t, err)

	overridden, _ := cfg.Get("database.host")
	assert.Equal(t, "test-db", overridden)

	port, _ := cfg.GetInt("database.port")
	assert.Equal(t, 1111, port)

	// Restore.
	restore()

	restored, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", restored)

	restoredPort, _ := cfg.GetInt("database.port")
	assert.Equal(t, 5432, restoredPort)
}

// ---------------------------------------------------------------------------
// Test: Reload picks up file changes
// ---------------------------------------------------------------------------

func TestReload(t *testing.T) {
	// Write a temp config file.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("host: original\nport: 1234"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
	)
	require.NoError(t, err)

	host, _ := cfg.Get("host")
	assert.Equal(t, "original", host)

	// Modify the file.
	require.NoError(t, os.WriteFile(cfgPath, []byte("host: updated\nport: 5678"), 0644))

	// Reload.
	require.NoError(t, cfg.Reload(context.Background()))

	host, _ = cfg.Get("host")
	assert.Equal(t, "updated", host)

	port, _ := cfg.GetInt("port")
	assert.Equal(t, 5678, port)
}

// ---------------------------------------------------------------------------
// Test: OnChange callback fires after reload
// ---------------------------------------------------------------------------

func TestOnChangeCallback(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("host: before"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
	)
	require.NoError(t, err)

	var changes []string
	cfg.OnChange(func(key string, oldVal, newVal any) {
		changes = append(changes, key)
	})

	require.NoError(t, os.WriteFile(cfgPath, []byte("host: after"), 0644))
	require.NoError(t, cfg.Reload(context.Background()))

	assert.Contains(t, changes, "host")
}

// ---------------------------------------------------------------------------
// Test: Export to JSON and YAML
// ---------------------------------------------------------------------------

func TestExport(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
	)
	require.NoError(t, err)

	// Export to JSON.
	jsonData, err := cfg.Export("json")
	require.NoError(t, err)
	assert.Contains(t, string(jsonData), `"host"`)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(jsonData, &parsed))
	assert.Contains(t, parsed, "api")

	// Export to YAML.
	yamlData, err := cfg.Export("yaml")
	require.NoError(t, err)
	assert.Contains(t, string(yamlData), "host:")
}

// ---------------------------------------------------------------------------
// Test: HTTP loader with a real (test) server
// ---------------------------------------------------------------------------

func TestHTTPLoader_RealServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"remote": map[string]any{
				"setting": "from-http",
				"count":   42,
			},
		})
	}))
	defer srv.Close()

	// Use flat configs (no environment sections) so merging is straightforward.
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewJSON("testdata/flat.json"),
			loader.NewHTTP(srv.URL),
		),
	)
	require.NoError(t, err)

	// HTTP values are merged on top.
	setting, err := cfg.Get("remote.setting")
	require.NoError(t, err)
	assert.Equal(t, "from-http", setting)

	// File values still available.
	apiHost, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", apiHost)
}

// ---------------------------------------------------------------------------
// Test: Git loader URL resolution (no network needed)
// ---------------------------------------------------------------------------

func TestGitLoader_URLResolution(t *testing.T) {
	l := cloud.NewGit(
		"https://github.com/myorg/config-repo",
		"services/app/config.yaml",
		cloud.WithGitBranch("release/v2"),
	)

	assert.Contains(t, l.Source(), "git:")
	assert.Contains(t, l.Source(), "release/v2")
}

// ---------------------------------------------------------------------------
// Test: Diff two environments
// ---------------------------------------------------------------------------

func TestDiffEnvironments(t *testing.T) {
	devCfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("staging"),
	)
	require.NoError(t, err)

	prodCfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	diffs := diff.Diff(devCfg.ToDict(), prodCfg.ToDict())
	assert.NotEmpty(t, diffs)

	summary := diff.Summary(diffs)
	assert.Greater(t, summary["modified"], 0)

	// Serialize to JSON — what the CLI would output.
	jsonStr, err := diff.ToJSON(diffs)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, "modified")
}

// ---------------------------------------------------------------------------
// Test: Drift detection
// ---------------------------------------------------------------------------

func TestDriftDetection(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	baseline := cfg.ToDict()

	// Simulate drift: someone changed a value.
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

// ---------------------------------------------------------------------------
// Test: Observability — metrics and events
// ---------------------------------------------------------------------------

func TestObservability(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	// Metrics.
	metrics := observe.NewMetrics(len(cfg.Keys()))

	start := time.Now()
	cfg.Get("database.host")
	metrics.RecordAccess("database.host", time.Since(start))

	cfg.Get("database.port")
	metrics.RecordAccess("database.port", time.Since(start))

	stats := metrics.Statistics()
	assert.Equal(t, 2, stats["accessed_keys"])

	// Events.
	emitter := observe.NewEventEmitter(nil)
	var reloadCount int
	emitter.On("reload", func(_ ...any) { reloadCount++ })

	emitter.Emit("reload", cfg.ToDict())
	assert.Equal(t, 1, reloadCount)
}

// ---------------------------------------------------------------------------
// Test: Versioning — save, list, rollback
// ---------------------------------------------------------------------------

func TestVersioning(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	dir := t.TempDir()
	vm := observe.NewVersionManager(dir, 100)

	// Save version 1.
	v1, err := vm.SaveVersion(cfg.ToDict(), map[string]any{"author": "deploy-bot", "env": "production"})
	require.NoError(t, err)
	assert.NotEmpty(t, v1.VersionID)

	// Modify config.
	require.NoError(t, cfg.Set("database.host", "new-host"))

	// Save version 2.
	v2, err := vm.SaveVersion(cfg.ToDict(), map[string]any{"author": "deploy-bot"})
	require.NoError(t, err)
	assert.NotEqual(t, v1.VersionID, v2.VersionID)

	// List versions.
	versions := vm.ListVersions()
	assert.Len(t, versions, 2)

	// Retrieve v1 — should have the original host.
	retrieved := vm.GetVersion(v1.VersionID)
	require.NotNil(t, retrieved)
	db := retrieved.Config["database"].(map[string]any)
	assert.Equal(t, "prod-db.example.com", db["host"])
}

// ---------------------------------------------------------------------------
// Test: Advanced merge strategies
// ---------------------------------------------------------------------------

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
		"database": merge.Replace, // replace entire database section
		"features": merge.Append,  // append feature lists
	})

	result := m.Merge(base, overlay)

	// database: replaced entirely (no port).
	db := result["database"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	_, hasPort := db["port"]
	assert.False(t, hasPort)

	// features: appended.
	assert.Equal(t, []any{"auth", "logging", "metrics"}, result["features"])

	// cache: deep merged (default strategy).
	cache := result["cache"].(map[string]any)
	assert.Equal(t, 3600, cache["ttl"])
	assert.Equal(t, true, cache["enabled"]) // preserved from base
}

// ---------------------------------------------------------------------------
// Test: MultiSecretStore fallback chain
// ---------------------------------------------------------------------------

func TestMultiSecretStore_FallbackChain(t *testing.T) {
	// Primary store has some secrets, secondary has others.
	primary := secret.NewDictStore(map[string]any{
		"db/password": "primary-pw",
	})
	secondary := secret.NewDictStore(map[string]any{
		"api/key":     "secondary-key",
		"db/password": "secondary-pw", // shadowed by primary
	})

	multi := secret.NewMultiStore([]confii.SecretStore{primary, secondary})
	ctx := context.Background()

	// Primary wins for shared keys.
	pw, err := multi.GetSecret(ctx, "db/password")
	require.NoError(t, err)
	assert.Equal(t, "primary-pw", pw)

	// Falls back to secondary.
	key, err := multi.GetSecret(ctx, "api/key")
	require.NoError(t, err)
	assert.Equal(t, "secondary-key", key)

	// Neither has it.
	_, err = multi.GetSecret(ctx, "missing")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: Secret resolver with prefix and TTL cache expiry
// ---------------------------------------------------------------------------

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

	// Resolve with prefix applied.
	val, err := resolver.Resolve(ctx, "${secret:db/password}")
	require.NoError(t, err)
	assert.Equal(t, "versioned-pw", val)

	// Update underlying store.
	_ = store.SetSecret(ctx, "prod/db/password", "new-pw")

	// Still cached.
	val, _ = resolver.Resolve(ctx, "${secret:db/password}")
	assert.Equal(t, "versioned-pw", val)

	// Wait for TTL.
	time.Sleep(60 * time.Millisecond)

	// Now picks up the new value.
	val, _ = resolver.Resolve(ctx, "${secret:db/password}")
	assert.Equal(t, "new-pw", val)
}

// ---------------------------------------------------------------------------
// Test: Concurrent access safety
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	// 50 concurrent readers.
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := cfg.Get("database.host"); err != nil {
					errs <- err
				}
				cfg.Keys()
				cfg.Has("database.port")
				cfg.ToDict()
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

// ---------------------------------------------------------------------------
// Test: Error handling — not found with suggestions
// ---------------------------------------------------------------------------

func TestNotFoundError(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	_, err = cfg.Get("database.hst") // typo
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigNotFound))
}

// ---------------------------------------------------------------------------
// Test: GetOr convenience methods
// ---------------------------------------------------------------------------

func TestGetOrConvenience(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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

// ---------------------------------------------------------------------------
// Test: Full pipeline — load, merge, validate, secret resolve, access
// ---------------------------------------------------------------------------

func TestFullPipeline(t *testing.T) {
	t.Setenv("PIPELINE_APP_VERSION", "2.0.0")

	// Secret store.
	store := secret.NewDictStore(map[string]any{
		"db_password": "pipeline-secret",
	})
	resolver := secret.NewResolver(store)

	// Build config with everything.
	cfg, err := confii.NewBuilder[any]().
		WithEnv("production").
		AddLoader(loader.NewYAML("testdata/base.yaml")).
		AddLoader(loader.NewYAML("testdata/overrides.yaml")).
		EnableDeepMerge().
		Build(context.Background())
	require.NoError(t, err)

	// Register hooks.
	cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())

	// Set a value with secret and env placeholders.
	require.NoError(t, cfg.Set("database.password", "${secret:db_password}"))
	require.NoError(t, cfg.Set("app.computed_version", "${PIPELINE_APP_VERSION}"))

	// Verify the full pipeline.
	pw, _ := cfg.Get("database.password")
	assert.Equal(t, "pipeline-secret", pw) // secret resolved

	ver, _ := cfg.Get("app.computed_version")
	assert.Equal(t, "2.0.0", ver) // env var expanded

	host, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", host) // from base.yaml production

	ssl, _ := cfg.GetBool("database.ssl")
	assert.True(t, ssl) // from overrides.yaml

	logLevel, _ := cfg.Get("app.log_level")
	assert.Equal(t, "warn", logLevel) // from overrides.yaml production
}

// ===========================================================================
// NEW GAP-CLOSING TESTS
// ===========================================================================

// ---------------------------------------------------------------------------
// Test: Composition directives (_include, _defaults)
// ---------------------------------------------------------------------------

func TestComposition_IncludeAndDefaults(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/with_include.yaml")),
	)
	require.NoError(t, err)

	// From _include: included.yaml.
	logLevel, err := cfg.Get("shared.log_level")
	require.NoError(t, err)
	assert.Equal(t, "info", logLevel)

	retries, err := cfg.GetInt("shared.max_retries")
	require.NoError(t, err)
	assert.Equal(t, 3, retries)

	// From _defaults.
	timeout, err := cfg.Get("timeout")
	require.NoError(t, err)
	assert.Equal(t, 30, timeout) // type casting hook converts "30" → int

	// From the config itself.
	name, err := cfg.Get("app.name")
	require.NoError(t, err)
	assert.Equal(t, "composed-app", name)

	// Directives should be removed.
	assert.False(t, cfg.Has("_include"))
	assert.False(t, cfg.Has("_defaults"))
}

// ---------------------------------------------------------------------------
// Test: Source tracking and introspection
// ---------------------------------------------------------------------------

func TestSourceTracking(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML("testdata/base.yaml"),
			loader.NewYAML("testdata/overrides.yaml"),
		),
		confii.WithEnv("production"),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	// GetSourceInfo.
	info := cfg.GetSourceInfo("database.host")
	assert.NotNil(t, info)
	assert.Equal(t, "prod-db.example.com", info.Value)

	// GetSourceStatistics.
	stats := cfg.GetSourceStatistics()
	assert.Greater(t, stats["total_keys"], 0)

	// FindKeysFromSource.
	keys := cfg.FindKeysFromSource("base.yaml")
	assert.NotEmpty(t, keys)

	// PrintDebugInfo.
	debugOutput := cfg.PrintDebugInfo("database.host")
	assert.Contains(t, debugOutput, "database.host")
	assert.Contains(t, debugOutput, "prod-db.example.com")

	// GetConflicts — overrides.yaml overrides some keys from base.yaml.
	conflicts := cfg.GetConflicts()
	// At minimum, database.ssl should be overridden (added by overrides).
	assert.NotNil(t, conflicts)
}

// ---------------------------------------------------------------------------
// Test: Explain
// ---------------------------------------------------------------------------

func TestExplain(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	explanation := cfg.Explain("database.host")
	assert.Equal(t, true, explanation["exists"])
	assert.Equal(t, "prod-db.example.com", explanation["current_value"])
	assert.Equal(t, "production", explanation["environment"])

	// Non-existent key.
	missing := cfg.Explain("nonexistent.key")
	assert.Equal(t, false, missing["exists"])
	assert.NotNil(t, missing["available_keys"])
}

// ---------------------------------------------------------------------------
// Test: Schema info
// ---------------------------------------------------------------------------

func TestSchemaInfo(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	info := cfg.Schema("database.host")
	assert.Equal(t, true, info["exists"])
	assert.Equal(t, "string", info["type"])
	assert.Equal(t, "prod-db.example.com", info["value"])
}

// ---------------------------------------------------------------------------
// Test: Layers
// ---------------------------------------------------------------------------

func TestLayers(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML("testdata/base.yaml"),
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

// ---------------------------------------------------------------------------
// Test: GenerateDocs
// ---------------------------------------------------------------------------

func TestGenerateDocs(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	// Markdown.
	md, err := cfg.GenerateDocs("markdown")
	require.NoError(t, err)
	assert.Contains(t, md, "api.host")
	assert.Contains(t, md, "Key")

	// JSON.
	jsonDocs, err := cfg.GenerateDocs("json")
	require.NoError(t, err)
	assert.Contains(t, jsonDocs, "api.host")
}

// ---------------------------------------------------------------------------
// Test: Set with override=false guard
// ---------------------------------------------------------------------------

func TestSet_OverrideFalse(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	// Should fail: key exists.
	err = cfg.Set("database.host", "new-host", confii.WithOverride(false))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Should succeed: key doesn't exist.
	err = cfg.Set("new.key", "value", confii.WithOverride(false))
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test: Extend (add loader at runtime)
// ---------------------------------------------------------------------------

func TestExtend(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	assert.False(t, cfg.Has("api.host"))

	// Extend with JSON config.
	err = cfg.Extend(context.Background(), loader.NewJSON("testdata/flat.json"))
	require.NoError(t, err)

	// New keys available.
	apiHost, err := cfg.Get("api.host")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", apiHost)

	// Original keys still available.
	dbHost, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", dbHost)
}

// ---------------------------------------------------------------------------
// Test: Export with TOML + file write
// ---------------------------------------------------------------------------

func TestExport_TOML(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("testdata/flat.json")),
	)
	require.NoError(t, err)

	data, err := cfg.Export("toml")
	require.NoError(t, err)
	assert.Contains(t, string(data), "host")
}

func TestExport_ToFile(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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

// ---------------------------------------------------------------------------
// Test: Reload with incremental and dry_run
// ---------------------------------------------------------------------------

func TestReload_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("host: original"), 0644)

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(cfgPath)),
	)
	require.NoError(t, err)

	_ = os.WriteFile(cfgPath, []byte("host: modified"), 0644)

	// Dry run should not apply changes.
	err = cfg.Reload(context.Background(), confii.WithDryRun(true), confii.WithIncremental(false))
	require.NoError(t, err)

	host, _ := cfg.Get("host")
	assert.Equal(t, "original", host) // unchanged
}

// ---------------------------------------------------------------------------
// Test: Observability integrated on Config
// ---------------------------------------------------------------------------

func TestObservability_IntegratedOnConfig(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	// Enable observability.
	metrics := cfg.EnableObservability()
	assert.NotNil(t, metrics)

	emitter := cfg.EnableEvents()
	assert.NotNil(t, emitter)

	emitter.On("reload", func(_ ...any) {})

	// GetMetrics should work now.
	m := cfg.GetMetrics()
	assert.NotNil(t, m)
}

// ---------------------------------------------------------------------------
// Test: Versioning integrated on Config
// ---------------------------------------------------------------------------

func TestVersioning_IntegratedOnConfig(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	dir := t.TempDir()
	cfg.EnableVersioning(dir, 100)

	// Save version.
	v1, err := cfg.SaveVersion(map[string]any{"author": "test"})
	require.NoError(t, err)
	assert.NotEmpty(t, v1.VersionID)

	// Modify config.
	cfg.Set("database.host", "rollback-target")

	// Save another version.
	v2, err := cfg.SaveVersion(nil)
	require.NoError(t, err)

	// Rollback to v1.
	err = cfg.RollbackToVersion(v1.VersionID)
	require.NoError(t, err)

	host, _ := cfg.Get("database.host")
	assert.Equal(t, "prod-db.example.com", host)

	_ = v2
}

// ---------------------------------------------------------------------------
// Test: Diff and DetectDrift on Config
// ---------------------------------------------------------------------------

func TestDiff_OnConfig(t *testing.T) {
	cfg1, _ := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("staging"),
	)
	cfg2, _ := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)

	diffs := cfg1.Diff(cfg2)
	assert.NotEmpty(t, diffs)
}

func TestDetectDrift_OnConfig(t *testing.T) {
	cfg, _ := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)

	intended := map[string]any{
		"database": map[string]any{"host": "expected-host", "port": 5432},
	}

	drifts := cfg.DetectDrift(intended)
	assert.NotEmpty(t, drifts) // host doesn't match
}

// ---------------------------------------------------------------------------
// Test: StopWatching
// ---------------------------------------------------------------------------

func TestStopWatching(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("testdata/base.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	// Should not panic even if no watcher is running.
	cfg.StopWatching()
}

// ---------------------------------------------------------------------------
// Test: ExportDebugReport
// ---------------------------------------------------------------------------

func TestExportDebugReport(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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
