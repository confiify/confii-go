package confii_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_BasicYAML(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host)

	port, err := cfg.GetInt("database.port")
	require.NoError(t, err)
	assert.Equal(t, 5432, port)

	debug, err := cfg.GetBool("debug")
	require.NoError(t, err)
	assert.True(t, debug)
}

func TestNew_EnvironmentResolution(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)

	// debug overridden to false in production.
	debug, err := cfg.GetBool("debug")
	require.NoError(t, err)
	assert.False(t, debug)
}

func TestNew_EnvSwitcher(t *testing.T) {
	t.Setenv("MY_ENV", "staging")

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnvSwitcher("MY_ENV"),
	)
	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.Env())

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "staging-db.example.com", host)
}

func TestConfig_MultipleLoaders(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML("loader/testdata/simple.yaml"),
			loader.NewJSON("loader/testdata/simple.json"),
		),
	)
	require.NoError(t, err)

	// JSON loader overrides YAML for database.host (both have "localhost").
	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host)
}

func TestConfig_Set(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	err = cfg.Set("database.host", "new-host")
	require.NoError(t, err)

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "new-host", host)
}

func TestConfig_Set_Frozen(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())

	err = cfg.Set("key", "value")
	assert.True(t, errors.Is(err, confii.ErrConfigFrozen))
}

func TestConfig_Has(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	assert.True(t, cfg.Has("database.host"))
	assert.False(t, cfg.Has("nonexistent"))
}

func TestConfig_Keys(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	allKeys := cfg.Keys()
	assert.Contains(t, allKeys, "database.host")
	assert.Contains(t, allKeys, "debug")

	dbKeys := cfg.Keys("database")
	assert.Contains(t, dbKeys, "host")
	assert.Contains(t, dbKeys, "port")
	assert.NotContains(t, dbKeys, "debug")
}

func TestConfig_ToDict(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	d := cfg.ToDict()
	assert.NotNil(t, d)
	assert.Contains(t, d, "database")
	assert.Contains(t, d, "debug")
}

func TestConfig_GetNotFound(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	_, err = cfg.Get("nonexistent")
	assert.True(t, errors.Is(err, confii.ErrConfigNotFound))
}

func TestConfig_GetOr(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	assert.Equal(t, "default", cfg.GetOr("missing", "default"))
	assert.Equal(t, "localhost", cfg.GetOr("database.host", "default"))
}

func TestConfig_SysenvFallback(t *testing.T) {
	t.Setenv("DATABASE_HOST", "env-host")

	cfg, err := confii.New[any](context.Background(),
		confii.WithSysenvFallback(true),
	)
	require.NoError(t, err)

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "env-host", host)
}

func TestConfig_SysenvFallback_WithPrefix(t *testing.T) {
	t.Setenv("MYAPP_DATABASE_HOST", "prefixed-host")

	cfg, err := confii.New[any](context.Background(),
		confii.WithSysenvFallback(true),
		confii.WithEnvPrefix("MYAPP"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prefixed-host", host)
}

func TestConfig_MustGet_Panics(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	assert.Panics(t, func() {
		cfg.MustGet("nonexistent")
	})
}

func TestConfig_String(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithEnv("test"),
	)
	require.NoError(t, err)

	s := cfg.String()
	assert.Contains(t, s, "test")
	assert.Contains(t, s, "Config(")
}

func TestConfig_ConcurrentAccess(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			cfg.Get("database.host")
		}()
		go func() {
			defer wg.Done()
			cfg.Keys()
		}()
		go func() {
			defer wg.Done()
			cfg.Has("debug")
		}()
	}
	wg.Wait()
}

func TestConfig_EnvExpander(t *testing.T) {
	t.Setenv("EXPANDED_VAL", "resolved")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvExpander(true),
	)
	require.NoError(t, err)

	// Set a value with env placeholder.
	err = cfg.Set("mykey", "${EXPANDED_VAL}")
	require.NoError(t, err)

	val, err := cfg.Get("mykey")
	require.NoError(t, err)
	assert.Equal(t, "resolved", val)
}

func TestConfig_NoLoaders(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Empty(t, cfg.Keys())
}
