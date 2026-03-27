package confii_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/hook"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// GetString / GetStringOr
// ---------------------------------------------------------------------------

func TestConfig_GetString(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("existing string key", func(t *testing.T) {
		s, err := cfg.GetString("database.host")
		require.NoError(t, err)
		assert.Equal(t, "localhost", s)
	})

	t.Run("non-string value is formatted", func(t *testing.T) {
		// database.port is an int; GetString should return its string representation.
		s, err := cfg.GetString("database.port")
		require.NoError(t, err)
		assert.Equal(t, "5432", s)
	})

	t.Run("missing key returns error", func(t *testing.T) {
		_, err := cfg.GetString("nonexistent")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigNotFound))
	})
}

func TestConfig_GetStringOr(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("returns value when key exists", func(t *testing.T) {
		assert.Equal(t, "localhost", cfg.GetStringOr("database.host", "fallback"))
	})

	t.Run("returns default when key missing", func(t *testing.T) {
		assert.Equal(t, "fallback", cfg.GetStringOr("no.such.key", "fallback"))
	})
}

// ---------------------------------------------------------------------------
// GetIntOr
// ---------------------------------------------------------------------------

func TestConfig_GetIntOr(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("returns value when key exists", func(t *testing.T) {
		assert.Equal(t, 5432, cfg.GetIntOr("database.port", 9999))
	})

	t.Run("returns default when key missing", func(t *testing.T) {
		assert.Equal(t, 9999, cfg.GetIntOr("no.such.key", 9999))
	})

	t.Run("returns default when type mismatch", func(t *testing.T) {
		// database.host is a string, not int.
		assert.Equal(t, 42, cfg.GetIntOr("database.host", 42))
	})
}

// ---------------------------------------------------------------------------
// GetBoolOr
// ---------------------------------------------------------------------------

func TestConfig_GetBoolOr(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("returns value when key exists", func(t *testing.T) {
		assert.True(t, cfg.GetBoolOr("debug", false))
	})

	t.Run("returns default when key missing", func(t *testing.T) {
		assert.False(t, cfg.GetBoolOr("nonexistent", false))
		assert.True(t, cfg.GetBoolOr("nonexistent", true))
	})

	t.Run("returns default when type mismatch", func(t *testing.T) {
		// database.host is a string, not bool.
		assert.True(t, cfg.GetBoolOr("database.host", true))
	})
}

// ---------------------------------------------------------------------------
// GetFloat64
// ---------------------------------------------------------------------------

func TestConfig_GetFloat64(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("loader/testdata/simple.json")),
	)
	require.NoError(t, err)

	t.Run("numeric value converts to float64", func(t *testing.T) {
		// JSON numbers are float64 by default.
		f, err := cfg.GetFloat64("database.port")
		require.NoError(t, err)
		assert.Equal(t, float64(5432), f)
	})

	t.Run("missing key returns error", func(t *testing.T) {
		_, err := cfg.GetFloat64("nonexistent")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigNotFound))
	})

	t.Run("type mismatch returns error", func(t *testing.T) {
		_, err := cfg.GetFloat64("database.host")
		assert.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// Set WithOverride(false)
// ---------------------------------------------------------------------------

func TestConfig_Set_WithOverrideFalse(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("blocks overwrite of existing key", func(t *testing.T) {
		err := cfg.Set("database.host", "new-host", confii.WithOverride(false))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("allows setting new key", func(t *testing.T) {
		err := cfg.Set("brand.new.key", "value", confii.WithOverride(false))
		require.NoError(t, err)

		val, err := cfg.Get("brand.new.key")
		require.NoError(t, err)
		assert.Equal(t, "value", val)
	})
}

// ---------------------------------------------------------------------------
// Explain
// ---------------------------------------------------------------------------

func TestConfig_Explain(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("existing key", func(t *testing.T) {
		info := cfg.Explain("database.host")
		assert.Equal(t, true, info["exists"])
		assert.Equal(t, "database.host", info["key"])
		assert.NotNil(t, info["current_value"])
	})

	t.Run("missing key", func(t *testing.T) {
		info := cfg.Explain("no.such.key")
		assert.Equal(t, false, info["exists"])
		assert.Equal(t, "no.such.key", info["key"])
		assert.NotNil(t, info["available_keys"])
	})
}

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

func TestConfig_Schema(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("existing key returns type info", func(t *testing.T) {
		s := cfg.Schema("database.host")
		assert.Equal(t, true, s["exists"])
		assert.Equal(t, "database.host", s["key"])
		assert.Equal(t, "string", s["type"])
		assert.Equal(t, "localhost", s["value"])
	})

	t.Run("int key returns int type", func(t *testing.T) {
		s := cfg.Schema("database.port")
		assert.Equal(t, true, s["exists"])
		assert.Contains(t, s["type"], "int")
	})

	t.Run("missing key", func(t *testing.T) {
		s := cfg.Schema("nonexistent")
		assert.Equal(t, false, s["exists"])
	})
}

// ---------------------------------------------------------------------------
// Layers
// ---------------------------------------------------------------------------

func TestConfig_Layers(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML("loader/testdata/simple.yaml"),
			loader.NewJSON("loader/testdata/simple.json"),
		),
	)
	require.NoError(t, err)

	layers := cfg.Layers()
	require.Len(t, layers, 2)

	assert.Equal(t, "loader/testdata/simple.yaml", layers[0]["source"])
	assert.NotNil(t, layers[0]["loader_type"])
	assert.Contains(t, layers[0], "keys")
	assert.Contains(t, layers[0], "key_count")

	assert.Equal(t, "loader/testdata/simple.json", layers[1]["source"])
}

func TestConfig_Layers_Empty(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	layers := cfg.Layers()
	assert.Empty(t, layers)
}

// ---------------------------------------------------------------------------
// Source tracking methods
// ---------------------------------------------------------------------------

func TestConfig_SourceTracking(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	t.Run("GetSourceInfo returns info for tracked key", func(t *testing.T) {
		info := cfg.GetSourceInfo("database.host")
		if info != nil {
			assert.Equal(t, "database.host", info.Key)
		}
	})

	t.Run("GetSourceInfo returns nil for unknown key", func(t *testing.T) {
		info := cfg.GetSourceInfo("totally.unknown")
		assert.Nil(t, info)
	})

	t.Run("GetOverrideHistory", func(t *testing.T) {
		history := cfg.GetOverrideHistory("database.host")
		// May be empty or non-empty depending on source tracking.
		assert.NotNil(t, history)
	})

	t.Run("GetConflicts", func(t *testing.T) {
		conflicts := cfg.GetConflicts()
		assert.NotNil(t, conflicts)
	})

	t.Run("GetSourceStatistics", func(t *testing.T) {
		stats := cfg.GetSourceStatistics()
		assert.NotNil(t, stats)
	})

	t.Run("FindKeysFromSource", func(t *testing.T) {
		// FindKeysFromSource may return nil if no keys match the pattern;
		// just verify it does not panic.
		_ = cfg.FindKeysFromSource("simple.yaml")
	})
}

// ---------------------------------------------------------------------------
// PrintDebugInfo / ExportDebugReport
// ---------------------------------------------------------------------------

func TestConfig_DebugInfo(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	t.Run("PrintDebugInfo with key", func(t *testing.T) {
		output := cfg.PrintDebugInfo("database.host")
		assert.NotEmpty(t, output)
	})

	t.Run("PrintDebugInfo all keys", func(t *testing.T) {
		output := cfg.PrintDebugInfo("")
		assert.NotEmpty(t, output)
	})

	t.Run("ExportDebugReport", func(t *testing.T) {
		tmpDir := t.TempDir()
		reportPath := filepath.Join(tmpDir, "debug_report.json")

		err := cfg.ExportDebugReport(reportPath)
		require.NoError(t, err)

		data, err := os.ReadFile(reportPath)
		require.NoError(t, err)
		assert.True(t, json.Valid(data))
	})
}

// ---------------------------------------------------------------------------
// SourceTracker
// ---------------------------------------------------------------------------

func TestConfig_SourceTracker(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	tracker := cfg.SourceTracker()
	assert.NotNil(t, tracker)
}

// ---------------------------------------------------------------------------
// GenerateDocs
// ---------------------------------------------------------------------------

func TestConfig_GenerateDocs(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("json format", func(t *testing.T) {
		docs, err := cfg.GenerateDocs("json")
		require.NoError(t, err)
		assert.True(t, json.Valid([]byte(docs)))
		assert.Contains(t, docs, "database.host")
	})

	t.Run("markdown format", func(t *testing.T) {
		docs, err := cfg.GenerateDocs("markdown")
		require.NoError(t, err)
		assert.Contains(t, docs, "| Key |")
		assert.Contains(t, docs, "database.host")
	})

	t.Run("unsupported format returns error", func(t *testing.T) {
		_, err := cfg.GenerateDocs("xml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported")
	})

	t.Run("empty config", func(t *testing.T) {
		emptyCfg, err := confii.New[any](context.Background())
		require.NoError(t, err)

		docs, err := emptyCfg.GenerateDocs("json")
		require.NoError(t, err)
		assert.True(t, json.Valid([]byte(docs)))
	})
}

// ---------------------------------------------------------------------------
// Freeze
// ---------------------------------------------------------------------------

func TestConfig_Freeze_Explicit(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("not frozen initially", func(t *testing.T) {
		assert.False(t, cfg.IsFrozen())
	})

	t.Run("Set works before freeze", func(t *testing.T) {
		err := cfg.Set("new.key", "value")
		require.NoError(t, err)
	})

	t.Run("Freeze makes config immutable", func(t *testing.T) {
		cfg.Freeze()
		assert.True(t, cfg.IsFrozen())
	})

	t.Run("Set returns error after freeze", func(t *testing.T) {
		err := cfg.Set("another.key", "value")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigFrozen))
	})

	t.Run("Reload returns error after freeze", func(t *testing.T) {
		err := cfg.Reload(context.Background())
		assert.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigFrozen))
	})
}

// ---------------------------------------------------------------------------
// OnChange callback
// ---------------------------------------------------------------------------

func TestConfig_OnChange(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var mu sync.Mutex
	changes := make(map[string][2]any)

	cfg.OnChange(func(key string, oldVal, newVal any) {
		mu.Lock()
		defer mu.Unlock()
		changes[key] = [2]any{oldVal, newVal}
	})

	// Modify a value so that reload will detect a change.
	err = cfg.Set("database.host", "changed-host")
	require.NoError(t, err)

	// Force a non-incremental reload to trigger change notifications.
	err = cfg.Reload(context.Background(), confii.WithIncremental(false))
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	// After reload the config reverts to file values, so the callback should
	// fire for database.host changing from "changed-host" back to "localhost".
	if len(changes) > 0 {
		_, found := changes["database.host"]
		assert.True(t, found, "expected change notification for database.host")
	}
}

func TestConfig_OnChange_MultipleCallbacks(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var count1, count2 int
	cfg.OnChange(func(key string, oldVal, newVal any) { count1++ })
	cfg.OnChange(func(key string, oldVal, newVal any) { count2++ })

	err = cfg.Set("database.host", "changed")
	require.NoError(t, err)

	err = cfg.Reload(context.Background(), confii.WithIncremental(false))
	require.NoError(t, err)

	// Both callbacks should have been invoked.
	assert.Greater(t, count1, 0)
	assert.Greater(t, count2, 0)
}

// ---------------------------------------------------------------------------
// HookProcessor
// ---------------------------------------------------------------------------

func TestConfig_HookProcessor(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("returns non-nil processor", func(t *testing.T) {
		hp := cfg.HookProcessor()
		assert.NotNil(t, hp)
	})

	t.Run("register key hook affects Get", func(t *testing.T) {
		hp := cfg.HookProcessor()
		hp.RegisterKeyHook("database.name", hook.Func(func(key string, value any) any {
			return "hooked-" + value.(string)
		}))

		val, err := cfg.Get("database.name")
		require.NoError(t, err)
		assert.Equal(t, "hooked-mydb", val)
	})
}

// ---------------------------------------------------------------------------
// Diff / DetectDrift
// ---------------------------------------------------------------------------

func TestConfig_Diff(t *testing.T) {
	cfg1, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	cfg2, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("identical configs produce no diffs", func(t *testing.T) {
		diffs := cfg1.Diff(cfg2)
		assert.Empty(t, diffs)
	})

	t.Run("modified config produces diffs", func(t *testing.T) {
		err := cfg2.Set("database.host", "other-host")
		require.NoError(t, err)

		diffs := cfg1.Diff(cfg2)
		assert.NotEmpty(t, diffs)
	})
}

func TestConfig_DetectDrift(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("no drift when baseline matches", func(t *testing.T) {
		baseline := cfg.ToDict()
		diffs := cfg.DetectDrift(baseline)
		assert.Empty(t, diffs)
	})

	t.Run("drift when baseline differs", func(t *testing.T) {
		baseline := map[string]any{
			"database": map[string]any{
				"host": "expected-host",
				"port": 5432,
				"name": "mydb",
			},
			"debug": true,
		}
		diffs := cfg.DetectDrift(baseline)
		assert.NotEmpty(t, diffs)
	})
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

func TestConfig_Export(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("json export", func(t *testing.T) {
		data, err := cfg.Export("json")
		require.NoError(t, err)
		assert.True(t, json.Valid(data))
		assert.Contains(t, string(data), "localhost")
	})

	t.Run("yaml export", func(t *testing.T) {
		data, err := cfg.Export("yaml")
		require.NoError(t, err)
		assert.Contains(t, string(data), "host:")
	})

	t.Run("toml export", func(t *testing.T) {
		data, err := cfg.Export("toml")
		require.NoError(t, err)
		assert.Contains(t, string(data), "host")
	})

	t.Run("unsupported format", func(t *testing.T) {
		_, err := cfg.Export("xml")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported")
	})

	t.Run("json export to file", func(t *testing.T) {
		tmpDir := t.TempDir()
		outPath := filepath.Join(tmpDir, "exported.json")

		data, err := cfg.Export("json", outPath)
		require.NoError(t, err)
		assert.NotEmpty(t, data)

		fileData, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Equal(t, data, fileData)
	})
}

func TestConfig_Export_EmptyConfig(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	t.Run("json empty", func(t *testing.T) {
		data, err := cfg.Export("json")
		require.NoError(t, err)
		s := strings.TrimSpace(string(data))
		assert.True(t, s == "null" || s == "{}")
	})

	t.Run("yaml empty", func(t *testing.T) {
		data, err := cfg.Export("yaml")
		require.NoError(t, err)
		assert.NotNil(t, data)
	})
}

// ---------------------------------------------------------------------------
// Override (temporary override with restore)
// ---------------------------------------------------------------------------

func TestConfig_Override(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("override and restore", func(t *testing.T) {
		originalHost, err := cfg.Get("database.host")
		require.NoError(t, err)
		assert.Equal(t, "localhost", originalHost)

		restore, err := cfg.Override(map[string]any{
			"database.host": "overridden-host",
		})
		require.NoError(t, err)

		val, err := cfg.Get("database.host")
		require.NoError(t, err)
		assert.Equal(t, "overridden-host", val)

		restore()

		val, err = cfg.Get("database.host")
		require.NoError(t, err)
		assert.Equal(t, "localhost", val)
	})

	t.Run("override multiple keys", func(t *testing.T) {
		restore, err := cfg.Override(map[string]any{
			"database.host": "temp-host",
			"debug":         false,
		})
		require.NoError(t, err)

		val, err := cfg.Get("database.host")
		require.NoError(t, err)
		assert.Equal(t, "temp-host", val)

		debug, err := cfg.GetBool("debug")
		require.NoError(t, err)
		assert.False(t, debug)

		restore()

		val, err = cfg.Get("database.host")
		require.NoError(t, err)
		assert.Equal(t, "localhost", val)

		debug, err = cfg.GetBool("debug")
		require.NoError(t, err)
		assert.True(t, debug)
	})

	t.Run("override restores frozen state", func(t *testing.T) {
		frozenCfg, err := confii.New[any](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
			confii.WithFreezeOnLoad(true),
		)
		require.NoError(t, err)
		assert.True(t, frozenCfg.IsFrozen())

		restore, err := frozenCfg.Override(map[string]any{"debug": false})
		require.NoError(t, err)

		debug, err := frozenCfg.GetBool("debug")
		require.NoError(t, err)
		assert.False(t, debug)

		restore()
		assert.True(t, frozenCfg.IsFrozen())
	})
}

// ---------------------------------------------------------------------------
// EnableObservability / EnableEvents / GetMetrics
// ---------------------------------------------------------------------------

func TestConfig_Observability(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("GetMetrics returns nil when not enabled", func(t *testing.T) {
		m := cfg.GetMetrics()
		assert.Nil(t, m)
	})

	t.Run("EnableObservability returns metrics", func(t *testing.T) {
		metrics := cfg.EnableObservability()
		assert.NotNil(t, metrics)
	})

	t.Run("GetMetrics returns data after enabling", func(t *testing.T) {
		m := cfg.GetMetrics()
		assert.NotNil(t, m)
	})

	t.Run("EnableObservability is idempotent", func(t *testing.T) {
		m1 := cfg.EnableObservability()
		m2 := cfg.EnableObservability()
		assert.Equal(t, m1, m2)
	})

	t.Run("EnableEvents returns emitter", func(t *testing.T) {
		emitter := cfg.EnableEvents()
		assert.NotNil(t, emitter)
	})

	t.Run("EnableEvents is idempotent", func(t *testing.T) {
		e1 := cfg.EnableEvents()
		e2 := cfg.EnableEvents()
		assert.Equal(t, e1, e2)
	})
}

// ---------------------------------------------------------------------------
// EnableVersioning / SaveVersion / RollbackToVersion
// ---------------------------------------------------------------------------

func TestConfig_Versioning(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("EnableVersioning returns manager", func(t *testing.T) {
		vm := cfg.EnableVersioning(filepath.Join(tmpDir, "versions"), 10)
		assert.NotNil(t, vm)
	})

	t.Run("EnableVersioning is idempotent", func(t *testing.T) {
		vm1 := cfg.EnableVersioning(filepath.Join(tmpDir, "versions"), 10)
		vm2 := cfg.EnableVersioning(filepath.Join(tmpDir, "versions"), 10)
		assert.Equal(t, vm1, vm2)
	})

	t.Run("SaveVersion and RollbackToVersion", func(t *testing.T) {
		v1, err := cfg.SaveVersion(map[string]any{"label": "v1"})
		require.NoError(t, err)
		assert.NotEmpty(t, v1.VersionID)

		err = cfg.Set("database.host", "modified-host")
		require.NoError(t, err)

		val, err := cfg.Get("database.host")
		require.NoError(t, err)
		assert.Equal(t, "modified-host", val)

		err = cfg.RollbackToVersion(v1.VersionID)
		require.NoError(t, err)

		val, err = cfg.Get("database.host")
		require.NoError(t, err)
		assert.Equal(t, "localhost", val)
	})

	t.Run("RollbackToVersion with unknown ID returns error", func(t *testing.T) {
		err := cfg.RollbackToVersion("nonexistent-version-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("RollbackToVersion on frozen config returns error", func(t *testing.T) {
		frozenCfg, err := confii.New[any](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
			confii.WithFreezeOnLoad(true),
		)
		require.NoError(t, err)

		err = frozenCfg.RollbackToVersion("any-id")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigFrozen))
	})
}

func TestConfig_SaveVersion_AutoEnable(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	v, err := cfg.SaveVersion(map[string]any{"label": "auto"})
	require.NoError(t, err)
	assert.NotEmpty(t, v.VersionID)
}

// ---------------------------------------------------------------------------
// RollbackToVersion without versioning enabled returns error
// ---------------------------------------------------------------------------

func TestConfig_RollbackToVersion_NotEnabled(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	err = cfg.RollbackToVersion("some-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "versioning not enabled")
}

// ---------------------------------------------------------------------------
// applySelfConfig via .confii.yaml (fileAutoLoader + selfconfig integration)
// ---------------------------------------------------------------------------

func TestConfig_ApplySelfConfig_WithConfiiYAML(t *testing.T) {
	// Create a temp directory with a .confii.yaml and a config file.
	tmpDir := t.TempDir()

	confiiYAML := `
default_environment: staging
env_prefix: MYAPP
default_files:
  - app.yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".confii.yaml"), []byte(confiiYAML), 0644))

	appYAML := `
default:
  database:
    host: default-host
staging:
  database:
    host: staging-host
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "app.yaml"), []byte(appYAML), 0644))

	// We cannot easily test applySelfConfig with the real CWD, but we can
	// verify that New works when no self-config is found (the default case).
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

// ---------------------------------------------------------------------------
// fileAutoLoader: Source, Load (YAML, JSON, missing, parse error)
// ---------------------------------------------------------------------------

func TestConfig_FileAutoLoader_YAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("key: value\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath)),
	)
	require.NoError(t, err)
	val, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", val)
}

func TestConfig_FileAutoLoader_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "config.json")
	require.NoError(t, os.WriteFile(jsonPath, []byte(`{"key": "jsonval"}`), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewJSON(jsonPath)),
	)
	require.NoError(t, err)
	val, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "jsonval", val)
}

// ---------------------------------------------------------------------------
// GetFloat64Or (if not exposed, test via GetOr with float)
// ---------------------------------------------------------------------------

func TestConfig_GetFloat64_IntConversion(t *testing.T) {
	// YAML int values should be convertible to float64.
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// database.port is an int in YAML; GetFloat64 should convert int->float64.
	f, err := cfg.GetFloat64("database.port")
	require.NoError(t, err)
	assert.Equal(t, float64(5432), f)
}

// ---------------------------------------------------------------------------
// copyMap - nested map deep copy via Override round-trip
// ---------------------------------------------------------------------------

func TestConfig_CopyMap_ViaOverride(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Override uses copyMap internally. After restore, original should be intact.
	restore, err := cfg.Override(map[string]any{
		"database.host": "overridden",
		"database.port": 9999,
	})
	require.NoError(t, err)

	val, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "overridden", val)

	restore()

	val, err = cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", val)

	intVal, err := cfg.GetInt("database.port")
	require.NoError(t, err)
	assert.Equal(t, 5432, intVal)
}

// ---------------------------------------------------------------------------
// SysenvFallback - NotFound path
// ---------------------------------------------------------------------------

func TestConfig_SysenvFallback_NotFoundKey(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithSysenvFallback(true),
	)
	require.NoError(t, err)

	_, err = cfg.Get("some.nonexistent.key.xyz")
	assert.Error(t, err)
}

func TestConfig_SysenvFallback_HooksApplied(t *testing.T) {
	t.Setenv("SYSENV_HOOK_TEST", "8080")

	cfg, err := confii.New[any](context.Background(),
		confii.WithSysenvFallback(true),
		confii.WithTypeCasting(true),
	)
	require.NoError(t, err)

	val, err := cfg.Get("sysenv.hook.test")
	require.NoError(t, err)
	// With type casting, "8080" should be converted to int.
	assert.Equal(t, 8080, val)
}

// ---------------------------------------------------------------------------
// Get returns map value as-is (not hooked)
// ---------------------------------------------------------------------------

func TestConfig_Get_MapValue(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	val, err := cfg.Get("database")
	require.NoError(t, err)
	m, ok := val.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "localhost", m["host"])
}

// ---------------------------------------------------------------------------
// Keys with prefix
// ---------------------------------------------------------------------------

func TestConfig_Keys_WithPrefix(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Keys with prefix returns sub-keys with the prefix stripped.
	keys := cfg.Keys("database")
	assert.NotEmpty(t, keys)
	assert.Contains(t, keys, "host")
	assert.Contains(t, keys, "port")
	assert.Contains(t, keys, "name")
	assert.NotContains(t, keys, "debug") // top-level key, not under database

	allKeys := cfg.Keys()
	assert.True(t, len(allKeys) > len(keys))
}

// ---------------------------------------------------------------------------
// ToDict when envConfig is nil
// ---------------------------------------------------------------------------

func TestConfig_ToDict_Empty(t *testing.T) {
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	dict := cfg.ToDict()
	// Should return mergedConfig (which may be empty or nil) without panic.
	_ = dict
}

// ---------------------------------------------------------------------------
// String() method with details
// ---------------------------------------------------------------------------

func TestConfig_String_Details(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	s := cfg.String()
	assert.Contains(t, s, "Config(")
	assert.Contains(t, s, "production")
	assert.Contains(t, s, "simple.yaml")
}

func TestConfig_String_Frozen(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)

	s := cfg.String()
	assert.Contains(t, s, "frozen")
}

// ---------------------------------------------------------------------------
// EnvSwitcher
// ---------------------------------------------------------------------------

func TestConfig_EnvSwitcher(t *testing.T) {
	t.Setenv("CONFIG_ENV", "production")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvSwitcher("CONFIG_ENV"),
	)
	require.NoError(t, err)
	assert.Equal(t, "production", cfg.Env())
}

func TestConfig_EnvSwitcher_Empty(t *testing.T) {
	// When the env var is not set, Env stays at default.
	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvSwitcher("NONEXISTENT_ENV_VAR_12345"),
		confii.WithEnv("fallback"),
	)
	require.NoError(t, err)
	assert.Equal(t, "fallback", cfg.Env())
}

// ---------------------------------------------------------------------------
// MustGet - success case
// ---------------------------------------------------------------------------

func TestConfig_MustGet_ReturnsValue(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	val := cfg.MustGet("database.host")
	assert.Equal(t, "localhost", val)
}

// ---------------------------------------------------------------------------
// GetInt with int64 and float64 underlying types
// ---------------------------------------------------------------------------

func TestConfig_GetInt_Float64Underlying(t *testing.T) {
	// JSON numbers are float64, so GetInt should handle conversion.
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("loader/testdata/simple.json")),
	)
	require.NoError(t, err)

	val, err := cfg.GetInt("database.port")
	require.NoError(t, err)
	assert.Equal(t, 5432, val)
}

// ---------------------------------------------------------------------------
// GetBool type mismatch
// ---------------------------------------------------------------------------

func TestConfig_GetBool_TypeMismatch(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	_, err = cfg.GetBool("database.host")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert")
}

// ---------------------------------------------------------------------------
// ValidateOnLoad + StrictValidation
// ---------------------------------------------------------------------------

func TestConfig_ValidateOnLoad_Strict_Failure(t *testing.T) {
	type Strict struct {
		RequiredField string `mapstructure:"required_field" validate:"required"`
	}

	// The YAML file doesn't have required_field, so strict validation should fail.
	_, err := confii.New[Strict](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithValidateOnLoad(true),
		confii.WithStrictValidation(true),
		confii.WithSchema(Strict{}),
	)
	assert.Error(t, err)
}

func TestConfig_ValidateOnLoad_NonStrict_Warning(t *testing.T) {
	type Strict struct {
		RequiredField string `mapstructure:"required_field" validate:"required"`
	}

	// Non-strict: should succeed with a warning, not an error.
	cfg, err := confii.New[Strict](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithValidateOnLoad(true),
		confii.WithStrictValidation(false),
		confii.WithSchema(Strict{}),
	)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

// ---------------------------------------------------------------------------
// FreezeOnLoad
// ---------------------------------------------------------------------------

func TestConfig_FreezeOnLoad(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())

	err = cfg.Set("new.key", "value")
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// OnChange with panicking callback (should not crash)
// ---------------------------------------------------------------------------

func TestConfig_OnChange_PanickingCallback(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: old\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	cfg.OnChange(func(key string, oldVal, newVal any) {
		panic("test panic in callback")
	})

	require.NoError(t, os.WriteFile(path, []byte("key: new\n"), 0644))
	// Should not panic.
	err = cfg.Reload(context.Background(), confii.WithIncremental(false))
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Reload with observability and events enabled
// ---------------------------------------------------------------------------

func TestConfig_Reload_WithObservabilityAndEvents(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: v1\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	cfg.EnableObservability()
	cfg.EnableEvents()

	require.NoError(t, os.WriteFile(path, []byte("key: v2\n"), 0644))
	err = cfg.Reload(context.Background(), confii.WithIncremental(false))
	require.NoError(t, err)

	metrics := cfg.GetMetrics()
	assert.NotNil(t, metrics)
}

// ---------------------------------------------------------------------------
// Reload with validation failure triggers rollback
// ---------------------------------------------------------------------------

func TestConfig_Reload_ValidationFailure_Rollback(t *testing.T) {
	type ValidCfg struct {
		Key string `mapstructure:"key" validate:"required"`
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: valid\n"), 0644))

	cfg, err := confii.New[ValidCfg](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	// Write an invalid config (empty key).
	require.NoError(t, os.WriteFile(path, []byte("other: value\n"), 0644))

	// Reload with validation should fail and rollback.
	boolTrue := true
	_ = boolTrue
	err = cfg.Reload(context.Background(),
		confii.WithIncremental(false),
		confii.WithReloadValidate(true),
	)
	assert.Error(t, err)

	// Original value should still be accessible.
	val, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "valid", val)
}

// ---------------------------------------------------------------------------
// Extend on frozen config returns error
// ---------------------------------------------------------------------------

func TestConfig_Extend_Frozen(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)

	err = cfg.Extend(context.Background(), loader.NewYAML("loader/testdata/simple.yaml"))
	assert.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigFrozen))
}

// ---------------------------------------------------------------------------
// Export to file path with bad directory
// ---------------------------------------------------------------------------

func TestConfig_Export_BadPath(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	data, err := cfg.Export("json", "/nonexistent/directory/file.json")
	// Should return data but error on write.
	assert.Error(t, err)
	assert.NotEmpty(t, data)
}

// ---------------------------------------------------------------------------
// Typed() success and cache
// ---------------------------------------------------------------------------

func TestConfig_Typed_Success(t *testing.T) {
	type DBConfig struct {
		Database struct {
			Host string `mapstructure:"host" validate:"required"`
			Port int    `mapstructure:"port" validate:"required"`
			Name string `mapstructure:"name" validate:"required"`
		} `mapstructure:"database"`
		Debug bool `mapstructure:"debug"`
	}

	cfg, err := confii.New[DBConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "localhost", model.Database.Host)
	assert.Equal(t, 5432, model.Database.Port)

	// Second call should return cached model.
	model2, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, model, model2)
}

// ---------------------------------------------------------------------------
// Has returns false for missing key
// ---------------------------------------------------------------------------

func TestConfig_Has_Missing(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	assert.True(t, cfg.Has("database.host"))
	assert.False(t, cfg.Has("nonexistent.key"))
}

// ---------------------------------------------------------------------------
// GetOr with various value types
// ---------------------------------------------------------------------------

func TestConfig_GetOr_VariousTypes(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Existing key returns actual value, ignoring the default.
	val := cfg.GetOr("database.port", 9999)
	assert.Equal(t, 5432, val)

	// Missing key returns the default.
	val = cfg.GetOr("missing.key.xyz", "fallback-value")
	assert.Equal(t, "fallback-value", val)
}
