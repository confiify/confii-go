// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/configmap"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_GetString(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("existing string key", func(t *testing.T) {
		s, err := cfg.GetString("database.host")
		require.NoError(t, err)
		assert.Equal(t, "localhost", s)
	})

	t.Run("non-string value is formatted", func(t *testing.T) {

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
	cfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_GetIntOr(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

		assert.Equal(t, 42, cfg.GetIntOr("database.host", 42))
	})
}

func TestConfig_GetBoolOr(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

		assert.True(t, cfg.GetBoolOr("database.host", true))
	})
}

func TestConfig_GetFloat64(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("loader/testdata/simple.json")),
	)
	require.NoError(t, err)

	t.Run("numeric value converts to float64", func(t *testing.T) {

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

func TestConfig_Set_WithOverrideFalse(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_Explain(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_Schema(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_Layers(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml"),
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
	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	layers := cfg.Layers()
	assert.Empty(t, layers)
}

func TestConfig_SourceTracking(t *testing.T) {

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.yaml")
	overlayPath := filepath.Join(tmpDir, "overlay.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("database:\n  host: base-host\n  port: 5432\n"), 0644))
	require.NoError(t, os.WriteFile(overlayPath, []byte("database:\n  host: overlay-host\n  user: admin\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(basePath),
			loader.NewYAML(overlayPath),
		),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	t.Run("GetSourceInfo returns info for tracked key", func(t *testing.T) {

		info := cfg.GetSourceInfo("database.host")
		require.NotNil(t, info, "expected source info for database.host to be tracked")
		assert.Equal(t, "database.host", info.Key)
		assert.NotEmpty(t, info.SourceFile, "tracked key must record an originating source file")
		assert.NotEmpty(t, info.LoaderType, "tracked key must record a loader type")

		assert.Contains(t, info.SourceFile, "overlay.yaml",
			"latest source for database.host must be the overlay file")
	})

	t.Run("GetSourceInfo returns nil for unknown key", func(t *testing.T) {
		info := cfg.GetSourceInfo("totally.unknown")
		assert.Nil(t, info)
	})

	t.Run("GetOverrideHistory records prior values for overridden keys", func(t *testing.T) {

		history := cfg.GetOverrideHistory("database.host")
		require.NotEmpty(t, history, "expected override history for database.host")
		assert.Contains(t, history[0].Source, "base.yaml",
			"first override entry should be the original base source")
	})

	t.Run("GetConflicts surfaces overridden keys", func(t *testing.T) {
		conflicts := cfg.GetConflicts()
		require.NotEmpty(t, conflicts, "expected at least one overridden key")
		_, ok := conflicts["database.host"]
		assert.True(t, ok, "database.host must appear in conflicts when overridden")

		_, conflicted := conflicts["database.port"]
		assert.False(t, conflicted, "database.port was never overridden")
	})

	t.Run("GetSourceStatistics returns concrete totals", func(t *testing.T) {
		stats := cfg.GetSourceStatistics()
		require.NotNil(t, stats)
		require.Contains(t, stats, "total_keys")
		totalKeys, ok := stats["total_keys"].(int)
		require.True(t, ok, "total_keys must be an int")
		assert.Greater(t, totalKeys, 0, "expected total_keys > 0")

		require.Contains(t, stats, "total_overrides")
		totalOverrides, ok := stats["total_overrides"].(int)
		require.True(t, ok, "total_overrides must be an int")
		assert.Greater(t, totalOverrides, 0, "expected total_overrides > 0 after a layered override")
	})

	t.Run("FindKeysFromSource returns keys for a real source", func(t *testing.T) {
		baseKeys := cfg.FindKeysFromSource("base.yaml")
		assert.Contains(t, baseKeys, "database.port",
			"database.port originates from base.yaml and was not overridden")

		overlayKeys := cfg.FindKeysFromSource("overlay.yaml")
		require.NotEmpty(t, overlayKeys, "expected at least one key tracked to overlay.yaml")
		assert.Contains(t, overlayKeys, "database.user",
			"database.user originates from overlay.yaml")
	})
}

func TestConfig_DebugInfo(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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
		info, err := os.Stat(reportPath)
		require.NoError(t, err)
		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
		}
	})
}

func TestConfig_SourceTracker(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	tracker := cfg.SourceTracker()
	assert.NotNil(t, tracker)
}

func TestConfig_GenerateDocs(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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
		emptyCfg, err := confii.NewWithContext[any](context.Background())
		require.NoError(t, err)

		docs, err := emptyCfg.GenerateDocs("json")
		require.NoError(t, err)
		assert.True(t, json.Valid([]byte(docs)))
	})
}

func TestConfig_Freeze_Explicit(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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
		err := cfg.ReloadWithContext(context.Background())
		assert.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigFrozen))
	})
}

func TestConfig_OnChange(t *testing.T) {
	t.Run("value-change reload fires callback with old and new values", func(t *testing.T) {

		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "cfg.yaml")
		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n"), 0644))

		cfg, err := confii.NewWithContext[any](context.Background(),
			confii.WithLoaders(loader.NewYAML(path)),
		)
		require.NoError(t, err)

		var mu sync.Mutex
		changes := make(map[string][2]any)
		cfg.OnChange(func(key string, oldVal, newVal any) {
			mu.Lock()
			defer mu.Unlock()
			changes[key] = [2]any{oldVal, newVal}
		})

		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: changed-host\n"), 0644))
		require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))

		mu.Lock()
		defer mu.Unlock()

		require.Contains(t, changes, "database.host",
			"expected OnChange to fire for database.host on value change")
		got := changes["database.host"]
		assert.Equal(t, "localhost", fmt.Sprintf("%v", got[0]),
			"old value must be localhost from initial load")
		assert.Equal(t, "changed-host", fmt.Sprintf("%v", got[1]),
			"new value must be changed-host from reloaded file")
	})

	t.Run("removed-key reload fires callback with old value and nil new value", func(t *testing.T) {

		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "cfg.yaml")
		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n  port: 5432\n"), 0644))

		cfg, err := confii.NewWithContext[any](context.Background(),
			confii.WithLoaders(loader.NewYAML(path)),
		)
		require.NoError(t, err)

		var mu sync.Mutex
		changes := make(map[string][2]any)
		cfg.OnChange(func(key string, oldVal, newVal any) {
			mu.Lock()
			defer mu.Unlock()
			changes[key] = [2]any{oldVal, newVal}
		})

		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n"), 0644))
		require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))

		mu.Lock()
		defer mu.Unlock()
		require.Contains(t, changes, "database.port",
			"expected OnChange to fire for removed key database.port")
		got := changes["database.port"]
		assert.NotNil(t, got[0], "old value of removed key must not be nil")
		assert.Nil(t, got[1], "new value of removed key must be nil")
	})
}

func TestConfig_OnChange_MultipleCallbacks(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var count1, count2 int
	cfg.OnChange(func(key string, oldVal, newVal any) { count1++ })
	cfg.OnChange(func(key string, oldVal, newVal any) { count2++ })

	err = cfg.Set("database.host", "changed")
	require.NoError(t, err)

	err = cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false))
	require.NoError(t, err)

	assert.Greater(t, count1, 0)
	assert.Greater(t, count2, 0)
}

func TestConfig_ConstructionHook(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithKeyHook("database.name", hook.Func(func(_ context.Context, _ string, value any) (any, error) {
			return "hooked-" + value.(string), nil
		})),
	)
	require.NoError(t, err)
	val, err := cfg.Get("database.name")
	require.NoError(t, err)
	assert.Equal(t, "hooked-mydb", val)
}

func TestConfig_Diff(t *testing.T) {
	cfg1, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	cfg2, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("identical configs produce no diffs", func(t *testing.T) {
		diffs, err := cfg1.Diff(cfg2)
		require.NoError(t, err)
		assert.Empty(t, diffs)
	})

	t.Run("modified config produces diffs", func(t *testing.T) {
		err := cfg2.Set("database.host", "other-host")
		require.NoError(t, err)

		diffs, err := cfg1.Diff(cfg2)
		require.NoError(t, err)
		assert.NotEmpty(t, diffs)
	})
}

func TestConfig_DetectDrift(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	t.Run("no drift when baseline matches", func(t *testing.T) {
		baseline, err := cfg.ToDict()
		require.NoError(t, err)
		diffs, err := cfg.DetectDrift(baseline)
		require.NoError(t, err)
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
		diffs, err := cfg.DetectDrift(baseline)
		require.NoError(t, err)
		assert.NotEmpty(t, diffs)
	})
}

func TestConfig_Export(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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
	cfg, err := confii.NewWithContext[any](context.Background())
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

func TestConfig_Override(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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
		frozenCfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_Observability(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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

func TestConfig_Versioning(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := confii.NewWithContext[any](context.Background(),
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
		frozenCfg, err := confii.NewWithContext[any](context.Background(),
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
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	v, err := cfg.SaveVersion(map[string]any{"label": "auto"})
	require.NoError(t, err)
	assert.NotEmpty(t, v.VersionID)
}

func TestConfig_RollbackToVersion_NotEnabled(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	err = cfg.RollbackToVersion("some-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "versioning not enabled")
}

func TestConfig_ApplySelfConfig_WithConfiiYAML(t *testing.T) {

	tmpDir := t.TempDir()

	confiiYAML := `
default_environment:staging
env_prefix:MYAPP
sources:  - type:yaml
    path:app.yaml
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

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestConfig_FileAutoLoader_YAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("key: value\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
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

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON(jsonPath)),
	)
	require.NoError(t, err)
	val, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "jsonval", val)
}

func TestConfig_GetFloat64_IntConversion(t *testing.T) {

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	f, err := cfg.GetFloat64("database.port")
	require.NoError(t, err)
	assert.Equal(t, float64(5432), f)
	assert.Equal(t, float64(5432), cfg.GetFloat64Or("database.port", 1.5))
	assert.Equal(t, 1.5, cfg.GetFloat64Or("database.host", 1.5))
	assert.Equal(t, 2.5, cfg.GetFloat64Or("missing", 2.5))
}

func TestConfig_CopyMap_ViaOverride(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

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

func TestConfig_SysenvFallback_NotFoundKey(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithSysenvFallback(true),
	)
	require.NoError(t, err)

	_, err = cfg.Get("some.nonexistent.key.xyz")
	assert.Error(t, err)
}

func TestConfig_SysenvFallback_HooksApplied(t *testing.T) {
	t.Setenv("SYSENV_HOOK_TEST", "8080")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithSysenvFallback(true),
		confii.WithTypeCasting(true),
	)
	require.NoError(t, err)

	val, err := cfg.Get("sysenv.hook.test")
	require.NoError(t, err)

	assert.Equal(t, 8080, val)
}

func TestConfig_Get_MapValue(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	val, err := cfg.Get("database")
	require.NoError(t, err)
	m, ok := val.(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "localhost", m["host"])
}

func TestConfig_Keys_WithPrefix(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	keys := cfg.Keys("database")
	assert.NotEmpty(t, keys)
	assert.Contains(t, keys, "database.host")
	assert.Contains(t, keys, "database.port")
	assert.Contains(t, keys, "database.name")
	assert.NotContains(t, keys, "debug")
	assert.NotContains(t, keys, "host", "Keys must not return a prefix-stripped key")

	allKeys := cfg.Keys()
	assert.True(t, len(allKeys) > len(keys))
}

func TestConfig_ToDict_Empty(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	dict, err := cfg.ToDict()
	require.NoError(t, err)

	_ = dict
}

func TestConfig_String_Details(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
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
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)

	s := cfg.String()
	assert.Contains(t, s, "frozen")
}

func TestConfig_EnvSwitcher(t *testing.T) {
	t.Setenv("CONFIG_ENV", "production")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvSwitcher("CONFIG_ENV"),
	)
	require.NoError(t, err)
	assert.Equal(t, "production", cfg.Env())
}

func TestConfig_EnvSwitcher_Empty(t *testing.T) {

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvSwitcher("NONEXISTENT_ENV_VAR_12345"),
		confii.WithEnv("fallback"),
	)
	require.NoError(t, err)
	assert.Equal(t, "fallback", cfg.Env())
}

func TestConfig_MustGet_ReturnsValue(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	val := cfg.MustGet("database.host")
	assert.Equal(t, "localhost", val)
}

func TestConfig_GetInt_Float64Underlying(t *testing.T) {

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON("loader/testdata/simple.json")),
	)
	require.NoError(t, err)

	val, err := cfg.GetInt("database.port")
	require.NoError(t, err)
	assert.Equal(t, 5432, val)
}

func TestConfig_GetBool_TypeMismatch(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	_, err = cfg.GetBool("database.host")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert")
}

func TestConfig_ValidateOnLoad_Strict_Failure(t *testing.T) {
	t.Run("struct schema with required tag fails strict validation", func(t *testing.T) {
		type Strict struct {
			RequiredField string `confii:"required_field" validate:"required"`
		}

		_, err := confii.NewWithContext[Strict](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
			confii.WithValidateOnLoad(true),
			confii.WithStrictValidation(true),
			confii.WithSchema(Strict{}),
		)
		require.Error(t, err)

		assert.True(t, errors.Is(err, confii.ErrConfigValidation),
			"strict validation failure must be a *ConfigError with ErrConfigValidation")
		assert.Contains(t, strings.ToLower(err.Error()), "required",
			"error must reference the required-field constraint")
	})

	t.Run("inline JSON schema dict surfaces schema-specific violations", func(t *testing.T) {
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"database": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"host": map[string]any{
							"type":    "string",
							"pattern": "^prod-.*$",
						},
					},
					"required": []any{"host"},
				},
			},
		}
		_, err := confii.NewWithContext[any](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
			confii.WithValidateOnLoad(true),
			confii.WithStrictValidation(true),
			confii.WithSchema(schema),
		)
		require.Error(t, err, "JSON schema pattern violation must be surfaced")
		assert.True(t, errors.Is(err, confii.ErrConfigValidation),
			"schema-violation error must wrap ErrConfigValidation")
		var ce *confii.ConfigError
		require.True(t, errors.As(err, &ce))
		raw, _ := ce.Context["schema_errors"].([]string)
		joined := ""
		for _, m := range raw {
			joined += strings.ToLower(m) + " "
		}
		assert.Contains(t, joined, "pattern",
			"schema_errors detail must reference the violated JSON Schema keyword")
	})
}

func TestConfig_ValidateOnLoad_NonStrict_Warning(t *testing.T) {
	type Strict struct {
		RequiredField string `confii:"required_field" validate:"required"`
	}

	cfg, err := confii.NewWithContext[Strict](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithValidateOnLoad(true),
		confii.WithStrictValidation(false),
		confii.WithSchema(Strict{}),
	)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestConfig_FreezeOnLoad(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())

	err = cfg.Set("new.key", "value")
	assert.Error(t, err)
}

func TestConfig_OnChange_PanickingCallback(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: old\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	cfg.OnChange(func(key string, oldVal, newVal any) {
		panic("test panic in callback")
	})

	require.NoError(t, os.WriteFile(path, []byte("key: new\n"), 0644))

	err = cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false))
	assert.NoError(t, err)
}

func TestConfig_Reload_WithObservabilityAndEvents(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: v1\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	cfg.EnableObservability()
	cfg.EnableEvents()

	require.NoError(t, os.WriteFile(path, []byte("key: v2\n"), 0644))
	err = cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false))
	require.NoError(t, err)

	metrics := cfg.GetMetrics()
	assert.NotNil(t, metrics)
}

func TestConfig_Reload_ValidationFailure_Rollback(t *testing.T) {
	type ValidCfg struct {
		Key string `confii:"key" validate:"required"`
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: valid\n"), 0644))

	cfg, err := confii.NewWithContext[ValidCfg](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("other: value\n"), 0644))

	boolTrue := true
	_ = boolTrue
	err = cfg.ReloadWithContext(context.Background(),
		confii.WithIncremental(false),
		confii.WithReloadValidate(true),
	)
	assert.Error(t, err)

	val, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "valid", val)
}

func TestConfig_Extend_Frozen(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithFreezeOnLoad(true),
	)
	require.NoError(t, err)

	err = cfg.ExtendWithContext(context.Background(), loader.NewYAML("loader/testdata/simple.yaml"))
	assert.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigFrozen))
}

func TestConfig_Export_BadPath(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	data, err := cfg.Export("json", "/nonexistent/directory/file.json")

	assert.Error(t, err)
	assert.NotEmpty(t, data)
}

func TestConfig_Typed_Success(t *testing.T) {
	type DBConfig struct {
		Database struct {
			Host string `confii:"host" validate:"required"`
			Port int    `confii:"port" validate:"required"`
			Name string `confii:"name" validate:"required"`
		} `confii:"database"`
		Debug bool `confii:"debug"`
	}

	cfg, err := confii.NewWithContext[DBConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "localhost", model.Database.Host)
	assert.Equal(t, 5432, model.Database.Port)

	model2, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, model, model2)
}

func TestConfig_Has_Missing(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	assert.True(t, cfg.Has("database.host"))
	assert.False(t, cfg.Has("nonexistent.key"))
}

func TestConfig_GetOr_VariousTypes(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	val := cfg.GetOr("database.port", 9999)
	assert.Equal(t, 5432, val)

	val = cfg.GetOr("missing.key.xyz", "fallback-value")
	assert.Equal(t, "fallback-value", val)
}

func TestConfig_ToDict_NoEnvironment(t *testing.T) {

	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	dict, err := cfg.ToDict()
	require.NoError(t, err)
	assert.NotNil(t, dict)
}

func TestConfig_Set_EmptyKeyPath(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	err = cfg.Set("", "value")
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigInvalid)
	assert.ErrorIs(t, err, configmap.ErrInvalidPath)
}

func TestConfig_Set_BadKeyPath_IntermediateNotMap(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	err = cfg.Set("database.host.deep.key", "value")
	assert.Error(t, err)
}

func TestConfig_Layers_DuplicateSources(t *testing.T) {
	yamlPath := "loader/testdata/simple.yaml"
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath),
			loader.NewYAML(yamlPath),
		),
	)
	require.NoError(t, err)

	layers := cfg.Layers()

	sourceCount := 0
	for _, l := range layers {
		if l["source"] == yamlPath {
			sourceCount++
		}
	}
	assert.Equal(t, 1, sourceCount, "duplicate source should be deduplicated")
}

func TestConfig_LoadWithComposeError(t *testing.T) {
	dir := t.TempDir()
	yamlContent := "_include:\n  - nonexistent_file.yaml\nkey: value\n"
	yamlPath := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(yamlContent), 0644))

	_, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath)),
	)
	require.Error(t, err, "composition error must surface under default Raise policy ")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath)),
		confii.WithOnError(confii.ErrorPolicyWarn),
	)
	require.NoError(t, err)
	val, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", val)
}

func TestConfig_StartWatching_NoFiles(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(&memLoader{source: "/nonexistent_dir_test_confii/no_such_file.yaml", data: map[string]any{"k": "v"}}),
		confii.WithDynamicReloading(true),
	)
	require.Error(t, err)
	require.Nil(t, cfg)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
}

type memLoader struct {
	source string
	data   map[string]any
}

func (l *memLoader) Load(_ context.Context) (map[string]any, error) {
	return l.data, nil
}

func (l *memLoader) Source() string { return l.source }

func TestRollback_DoesNotAliasSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	cfg.EnableVersioning(filepath.Join(tmpDir, "versions"), 10)

	v1, err := cfg.SaveVersion(map[string]any{"label": "baseline"})
	require.NoError(t, err)

	originalHost, err := cfg.Get("database.host")
	require.NoError(t, err)
	require.Equal(t, "localhost", originalHost)

	require.NoError(t, cfg.Set("database.host", "mutation-A"))

	require.NoError(t, cfg.RollbackToVersion(v1.VersionID))
	rolled, err := cfg.Get("database.host")
	require.NoError(t, err)
	require.Equal(t, "localhost", rolled)

	require.NoError(t, cfg.Set("database.host", "mutation-B"))

	require.NoError(t, cfg.RollbackToVersion(v1.VersionID))
	rolledAgain, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", rolledAgain,
		"rollback target was mutated through aliased snapshot")
}

func TestConfig_GetConflicts_ReturnsDefensiveCopy(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.yaml")
	overlayPath := filepath.Join(tmpDir, "overlay.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte("database:\n  host: base-host\n  port: 5432\n"), 0644))
	require.NoError(t, os.WriteFile(overlayPath, []byte("database:\n  host: overlay-host\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(basePath),
			loader.NewYAML(overlayPath),
		),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	first := cfg.GetConflicts()
	require.NotEmpty(t, first, "expected at least one overridden key")
	info, ok := first["database.host"]
	require.True(t, ok, "database.host must appear in conflicts")
	require.NotNil(t, info)

	originalSource := info.SourceFile
	originalValue := info.Value
	originalOverrideCount := info.OverrideCount

	info.SourceFile = "tampered.yaml"
	info.Value = "tampered"
	info.OverrideCount = 999
	if len(info.History) > 0 {
		info.History[0].Source = "evil"
	}

	second := cfg.GetConflicts()
	got, ok := second["database.host"]
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, originalSource, got.SourceFile,
		"SourceFile mutation on Config.GetConflicts result must not leak into tracker")
	assert.Equal(t, originalValue, got.Value,
		"Value mutation on Config.GetConflicts result must not leak into tracker")
	assert.Equal(t, originalOverrideCount, got.OverrideCount,
		"OverrideCount mutation on Config.GetConflicts result must not leak into tracker")
	if len(got.History) > 0 {
		assert.NotEqual(t, "evil", got.History[0].Source,
			"History mutation on Config.GetConflicts result must not leak into tracker")
	}
	assert.NotSame(t, info, got,
		"successive Config.GetConflicts calls must return distinct *SourceInfo copies")
}
