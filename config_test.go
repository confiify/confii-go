package confii_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
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

	// G30 (Wave 21): Keys(prefix) now returns FULL prefixed keys so the
	// documented `for _, k := range cfg.Keys(p) { cfg.Get(k) }` loop
	// works without manually re-prepending the prefix. Pre-Wave 21 this
	// returned ["host","port",...]; the suffix-only form is recoverable
	// at the call site via strings.TrimPrefix when needed.
	dbKeys := cfg.Keys("database")
	assert.Contains(t, dbKeys, "database.host")
	assert.Contains(t, dbKeys, "database.port")
	assert.NotContains(t, dbKeys, "debug")
	assert.NotContains(t, dbKeys, "host", "Keys must not return prefix-stripped form post-Wave 21")
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
			_, _ = cfg.Get("database.host")
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

// valueLoader is a value-typed (non-pointer) Loader implementation used to
// verify that Config does not panic when reflecting on a non-pointer loader.
// See gap G09: reflect.TypeOf(l).Elem() previously panicked for value-typed
// loaders because Elem is only valid on pointer/array/chan/map/slice kinds.
type valueLoader struct {
	source string
	data   map[string]any
}

func (v valueLoader) Source() string { return v.source }
func (v valueLoader) Load(_ context.Context) (map[string]any, error) {
	return v.data, nil
}

func TestConfig_AcceptsValueTypedLoader(t *testing.T) {
	vl := valueLoader{
		source: "value-loader://test",
		data: map[string]any{
			"vl_key":  "vl_value",
			"shared":  "from-value-loader",
			"numeric": 42,
		},
	}

	require.NotPanics(t, func() {
		cfg, err := confii.New[any](context.Background(),
			confii.WithLoaders(vl),
		)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		got, err := cfg.Get("vl_key")
		require.NoError(t, err)
		assert.Equal(t, "vl_value", got)

		assert.True(t, cfg.Has("shared"))
		assert.True(t, cfg.Has("numeric"))
	})
}

func TestConfig_LayersDoesNotPanicOnValueLoader(t *testing.T) {
	vl := valueLoader{
		source: "value-loader://layers",
		data:   map[string]any{"vl_layered": true},
	}

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(vl),
	)
	require.NoError(t, err)

	var layers []map[string]any
	require.NotPanics(t, func() {
		layers = cfg.Layers()
	})
	require.NotEmpty(t, layers)

	var found bool
	for _, layer := range layers {
		if layer["source"] == "value-loader://layers" {
			assert.Equal(t, "valueLoader", layer["loader_type"])
			found = true
		}
	}
	assert.True(t, found, "expected a layer entry for the value-typed loader")

	// Explain on a key tracked through the value-typed loader must not panic.
	// The loader_type surfaced may reflect a later tracking pass (e.g. the
	// EnvironmentHandler resolution step), but the override history should
	// still record the original value-typed loader by name.
	require.NotPanics(t, func() {
		info := cfg.Explain("vl_layered")
		assert.Equal(t, true, info["exists"])
		if history, ok := info["override_history"].([]map[string]any); ok {
			var sawValueLoader bool
			for _, h := range history {
				if h["loader_type"] == "valueLoader" {
					sawValueLoader = true
					break
				}
			}
			assert.True(t, sawValueLoader, "override history should record valueLoader")
		}
	})
}

func TestConfig_ExtendDoesNotPanicOnValueLoader(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	vl := valueLoader{
		source: "value-loader://extend",
		data:   map[string]any{"extended_key": "extended_value"},
	}

	require.NotPanics(t, func() {
		require.NoError(t, cfg.Extend(context.Background(), vl))
	})

	got, err := cfg.Get("extended_key")
	require.NoError(t, err)
	assert.Equal(t, "extended_value", got)

	// The Extend path also feeds source tracking; ensure the value-typed
	// loader's name was captured rather than tripping the old reflect panic.
	info := cfg.Explain("extended_key")
	assert.Equal(t, true, info["exists"])
	assert.Equal(t, "valueLoader", info["loader_type"])
}
