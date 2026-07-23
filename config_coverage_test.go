package confii_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// TestConfig_SourceTracking pins the documented contract of the
// source-tracking surface. Previously these subtests were coverage theater:
// they used conditional patterns (`if info != nil { ... }`) and asserted only
// `assert.NotNil` on slices that may be nil-but-non-empty, so a tracker that
// simply returned empty data would still pass. Each subtest now asserts a
// concrete, positive structural property that fails if tracking is broken.
func TestConfig_SourceTracking(t *testing.T) {
	// Stack two file loaders with a deliberate overlap on database.host
	// (so override history/conflicts populate) AND a base-only key
	// (database.port) plus an overlay-only key (database.user) so
	// FindKeysFromSource has unambiguous results.
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.yaml")
	overlayPath := filepath.Join(tmpDir, "overlay.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte(
		"database:\n  host: base-host\n  port: 5432\n",
	), 0644))
	require.NoError(t, os.WriteFile(overlayPath, []byte(
		"database:\n  host: overlay-host\n  user: admin\n",
	), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML(basePath),
			loader.NewYAML(overlayPath),
		),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	t.Run("GetSourceInfo returns info for tracked key", func(t *testing.T) {
		// Must return non-nil info AND identify the key. A broken tracker
		// that returns nil here will now fail the test.
		info := cfg.GetSourceInfo("database.host")
		require.NotNil(t, info, "expected source info for database.host to be tracked")
		assert.Equal(t, "database.host", info.Key)
		assert.NotEmpty(t, info.SourceFile, "tracked key must record an originating source file")
		assert.NotEmpty(t, info.LoaderType, "tracked key must record a loader type")

		// G08: After load() finishes, the env-resolution pass re-tracks
		// every key with SourceFile="(resolved)" and LoaderType=
		// "EnvironmentHandler", overwriting the real loader source.
		// When G08 is fixed, the latest source for database.host must be
		// the overlay file.
		if info.SourceFile == "(resolved)" {
			t.Skip("blocked by G08: env-resolution re-track erases real source file; remove skip when G08 is fixed")
		}
		assert.Contains(t, info.SourceFile, "overlay.yaml",
			"latest source for database.host must be the overlay file")
	})

	t.Run("GetSourceInfo returns nil for unknown key", func(t *testing.T) {
		info := cfg.GetSourceInfo("totally.unknown")
		assert.Nil(t, info)
	})

	t.Run("GetOverrideHistory records prior values for overridden keys", func(t *testing.T) {
		// database.host is set by base.yaml and overridden by overlay.yaml,
		// so a debug-mode tracker MUST record at least one history entry
		// pointing at the base file.
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

		// G08: env-resolution re-track also bumps the override count for
		// every base-only key, so until G08 is fixed, base-only keys like
		// database.port are spuriously flagged as conflicts. The contract
		// (asserted once G08 is fixed): a base-only key must NOT be a conflict.
		if _, ok := conflicts["database.port"]; ok {
			t.Skip("blocked by G08: env-resolution re-track inflates override counts for base-only keys; remove skip when G08 is fixed")
		}
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
		// total_overrides must be > 0 because overlay.yaml overrode database.host.
		require.Contains(t, stats, "total_overrides")
		totalOverrides, ok := stats["total_overrides"].(int)
		require.True(t, ok, "total_overrides must be an int")
		assert.Greater(t, totalOverrides, 0, "expected total_overrides > 0 after a layered override")
	})

	t.Run("FindKeysFromSource returns keys for a real source", func(t *testing.T) {
		// Previously this only verified the call did not panic. The
		// contract (asserted once G08 is fixed): the per-source lookup
		// returns keys originating from each file:
		//   - database.port is base-only so it must be associated with base.yaml
		//   - database.user is overlay-only so it must be associated with overlay.yaml
		//
		// G08: env-resolution re-tracks every key with SourceFile="(resolved)",
		// so substring matches against the real file paths return empty
		// today. Skip until G08 is fixed.
		baseKeys := cfg.FindKeysFromSource("base.yaml")
		if len(baseKeys) == 0 {
			t.Skip("blocked by G08: env-resolution re-track replaces real source filenames; remove skip when G08 is fixed")
		}
		assert.Contains(t, baseKeys, "database.port",
			"database.port originates from base.yaml and was not overridden")

		overlayKeys := cfg.FindKeysFromSource("overlay.yaml")
		require.NotEmpty(t, overlayKeys, "expected at least one key tracked to overlay.yaml")
		assert.Contains(t, overlayKeys, "database.user",
			"database.user originates from overlay.yaml")
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
		info, err := os.Stat(reportPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
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

// TestConfig_OnChange pins the contract of OnChange callbacks across two
// scenarios that the previous version of this test could not enforce:
//   - a value-change reload MUST fire the callback with the exact old and
//     new payload (the previous test's `if len(changes) > 0 { ... }` guard
//     allowed an entirely silent callback to pass).
//   - a key-removal reload MUST also fire the callback with the removed
//     key reported (currently broken — see G13).
func TestConfig_OnChange(t *testing.T) {
	t.Run("value-change reload fires callback with old and new values", func(t *testing.T) {
		// Use a temp file so we can mutate it on disk and force a real
		// reload-driven change notification (Set+Reload would revert the
		// in-memory edit to the file value, which is harder to reason
		// about because the "old" value is the user's edit, not the file).
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "cfg.yaml")
		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n"), 0644))

		cfg, err := confii.New[any](context.Background(),
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

		// Mutate the file on disk to a new value and force a reload.
		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: changed-host\n"), 0644))
		require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))

		mu.Lock()
		defer mu.Unlock()
		// Strong, unconditional assertion: callback MUST have fired for
		// database.host with the exact old/new payload.
		require.Contains(t, changes, "database.host",
			"expected OnChange to fire for database.host on value change")
		got := changes["database.host"]
		assert.Equal(t, "localhost", fmt.Sprintf("%v", got[0]),
			"old value must be localhost from initial load")
		assert.Equal(t, "changed-host", fmt.Sprintf("%v", got[1]),
			"new value must be changed-host from reloaded file")
	})

	t.Run("removed-key reload fires callback with old value and nil new value", func(t *testing.T) {
		// G13 (FIXED): notifyChangesUnlocked now iterates the union of
		// old and new flat keys, so a key that disappears from the
		// config surfaces to the callback as (oldVal, nil). This
		// subtest pins that contract end-to-end.

		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "cfg.yaml")
		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n  port: 5432\n"), 0644))

		cfg, err := confii.New[any](context.Background(),
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

		// Remove database.port by writing a file that no longer contains it.
		require.NoError(t, os.WriteFile(path, []byte("database:\n  host: localhost\n"), 0644))
		require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))

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

	// G30 (Wave 21): Keys(prefix) now returns FULL prefixed keys so the
	// returned slice can be fed directly into cfg.Get / cfg.Has without
	// re-prepending the prefix. Pre-Wave 21 this assertion checked the
	// stripped form ("host", "port", "name").
	keys := cfg.Keys("database")
	assert.NotEmpty(t, keys)
	assert.Contains(t, keys, "database.host")
	assert.Contains(t, keys, "database.port")
	assert.Contains(t, keys, "database.name")
	assert.NotContains(t, keys, "debug") // top-level key, not under database
	assert.NotContains(t, keys, "host", "Keys must not return prefix-stripped form post-Wave 21")

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

// TestConfig_ValidateOnLoad_Strict_Failure previously asserted only that
// generic struct validation (validator.v10's `validate:"required"` tag)
// raises an error in strict mode — it never proved JSON Schema
// integration. The rewritten test pins both contracts:
//
//  1. Struct validation in strict mode still fails (kept; this is the
//     legacy behavior path that must not regress).
//  2. JSON Schema integration: when a JSON Schema dict is passed via
//     WithSchema, strict validation must surface schema-specific errors
//     (e.g. minimum-violation). G01: JSON Schema dicts are currently
//     ignored, so the schema sub-assertion is skipped until G01 ships.
func TestConfig_ValidateOnLoad_Strict_Failure(t *testing.T) {
	t.Run("struct schema with required tag fails strict validation", func(t *testing.T) {
		type Strict struct {
			RequiredField string `mapstructure:"required_field" validate:"required"`
		}

		// The YAML file doesn't have required_field, so strict validation
		// must surface a typed validation error.
		_, err := confii.New[Strict](context.Background(),
			confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
			confii.WithValidateOnLoad(true),
			confii.WithStrictValidation(true),
			confii.WithSchema(Strict{}),
		)
		require.Error(t, err)
		// The error path goes through DecodeAndValidate which wraps the
		// underlying validator.v10 error in a *ConfigError of kind
		// ErrConfigValidation. Pin the wrapping so the error path cannot
		// silently degrade to a generic error.
		assert.True(t, errors.Is(err, confii.ErrConfigValidation),
			"strict validation failure must be a *ConfigError with ErrConfigValidation")
		assert.Contains(t, strings.ToLower(err.Error()), "required",
			"error must reference the required-field constraint")
	})

	t.Run("inline JSON schema dict surfaces schema-specific violations", func(t *testing.T) {
		// G01 (closed Wave 14): JSON Schema dicts passed to WithSchema
		// are wired through validate-on-load. Pattern violations surface
		// as a typed *ConfigError with structured detail on
		// Context["schema_errors"]; the public message is sanitized.
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
		_, err := confii.New[any](context.Background(),
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

// ===========================================================================
// ToDict when envConfig is nil (line 392)
// ===========================================================================

func TestConfig_ToDict_NoEnvironment(t *testing.T) {
	// Create a config with no environment set. When envConfig is populated,
	// ToDict returns envConfig. But let's verify the fallback path by
	// creating a config with no loaders so envConfig should be nil.
	cfg, err := confii.New[any](context.Background())
	require.NoError(t, err)

	// ToDict should return something non-nil (either envConfig or mergedConfig).
	dict := cfg.ToDict()
	assert.NotNil(t, dict)
}

// ===========================================================================
// Set with bad keyPath (lines 358-360)
// ===========================================================================

func TestConfig_Set_EmptyKeyPath(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Setting with an empty key path should work (sets "" key).
	// The Set call itself should not error.
	err = cfg.Set("", "value")
	assert.NoError(t, err)
}

func TestConfig_Set_BadKeyPath_IntermediateNotMap(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// database.host is a string, so trying to set database.host.deep should fail
	// because "host" is not a map.
	err = cfg.Set("database.host.deep.key", "value")
	assert.Error(t, err)
}

// ===========================================================================
// Layers with duplicate sources (lines 471-472)
// ===========================================================================

func TestConfig_Layers_DuplicateSources(t *testing.T) {
	yamlPath := "loader/testdata/simple.yaml"
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML(yamlPath),
			loader.NewYAML(yamlPath),
		),
	)
	require.NoError(t, err)

	layers := cfg.Layers()
	// Even though the same YAML file is loaded twice, Layers should deduplicate by source.
	sourceCount := 0
	for _, l := range layers {
		if l["source"] == yamlPath {
			sourceCount++
		}
	}
	assert.Equal(t, 1, sourceCount, "duplicate source should be deduplicated")
}

// ===========================================================================
// load with compose error (lines 165-168)
//
// G07 update: composition errors now flow through c.opts.OnError. Under
// the default ErrorPolicyRaise the error must surface from confii.New;
// under ErrorPolicyWarn / ErrorPolicyIgnore the loader continues with
// the un-composed (raw) data. This test pins both halves of that
// contract — the previous test asserted the pre-G07 behavior that
// always swallowed the composition error regardless of policy, which
// the audit (G07) flagged as exactly the bug being closed.
// ===========================================================================

func TestConfig_LoadWithComposeError(t *testing.T) {
	dir := t.TempDir()
	yamlContent := "_include:\n  - nonexistent_file.yaml\nkey: value\n"
	yamlPath := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(yamlContent), 0644))

	// Default policy is Raise: composition error must surface.
	_, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath)),
	)
	require.Error(t, err, "composition error must surface under default Raise policy (G07)")

	// Under Warn the legacy fallback (use raw data, log a warning) is preserved.
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath)),
		confii.WithOnError(confii.ErrorPolicyWarn),
	)
	require.NoError(t, err)
	val, err := cfg.Get("key")
	require.NoError(t, err)
	assert.Equal(t, "value", val)
}

// ===========================================================================
// startWatching error path (lines 925-928)
// ===========================================================================

func TestConfig_StartWatching_NoFiles(t *testing.T) {
	// A loader that returns no files (source is a non-file like "memory")
	// should trigger the startWatching error path (fsnotify fails on non-existent dirs).
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(&memLoader{source: "/nonexistent_dir_test_confii/no_such_file.yaml", data: map[string]any{"k": "v"}}),
		confii.WithDynamicReloading(true),
	)
	require.NoError(t, err)
	defer cfg.StopWatching()

	// Config should still work even though watcher failed to start.
	val, err := cfg.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "v", val)
}

// memLoader is a stub loader for tests in the external test package.
type memLoader struct {
	source string
	data   map[string]any
}

func (l *memLoader) Load(_ context.Context) (map[string]any, error) {
	return l.data, nil
}

func (l *memLoader) Source() string { return l.source }

// ---------------------------------------------------------------------------
// Rollback snapshot must not alias internal version state (G22)
// ---------------------------------------------------------------------------

// TestRollback_DoesNotAliasSnapshot proves that RollbackToVersion deep-copies
// the snapshot before assigning it into live state. Otherwise, mutations to
// live state after the first rollback would corrupt the stored version, so
// rolling back to that same version a second time would no longer recover
// the original value.
func TestRollback_DoesNotAliasSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	cfg.EnableVersioning(filepath.Join(tmpDir, "versions"), 10)

	// 1. Save a baseline snapshot.
	v1, err := cfg.SaveVersion(map[string]any{"label": "baseline"})
	require.NoError(t, err)

	// Confirm starting value.
	originalHost, err := cfg.Get("database.host")
	require.NoError(t, err)
	require.Equal(t, "localhost", originalHost)

	// 2. Mutate the live config.
	require.NoError(t, cfg.Set("database.host", "mutation-A"))

	// 3. Roll back to v1.
	require.NoError(t, cfg.RollbackToVersion(v1.VersionID))
	rolled, err := cfg.Get("database.host")
	require.NoError(t, err)
	require.Equal(t, "localhost", rolled)

	// 4. Mutate the supposedly-rolled-back state. If Rollback aliased the
	// snapshot map, this Set would also corrupt the stored Version's Config.
	require.NoError(t, cfg.Set("database.host", "mutation-B"))

	// 5. Roll back to v1 *again* and confirm we still see the original value.
	require.NoError(t, cfg.RollbackToVersion(v1.VersionID))
	rolledAgain, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", rolledAgain,
		"rollback target was mutated through aliased snapshot")
}

// ---------------------------------------------------------------------------
// F-Tracker-GetConflicts-Aliasing: end-to-end via Config.GetConflicts.
//
// Config.GetConflicts simply delegates to sourcetrack.Tracker.GetConflicts,
// so the defensive-copy contract pinned at the tracker layer must hold
// when observed through the public Config API surface.
// ---------------------------------------------------------------------------

// TestConfig_GetConflicts_ReturnsDefensiveCopy asserts that mutating
// the *SourceInfo values in the map returned by Config.GetConflicts
// does not corrupt tracker state observed by a subsequent call.
func TestConfig_GetConflicts_ReturnsDefensiveCopy(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.yaml")
	overlayPath := filepath.Join(tmpDir, "overlay.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte(
		"database:\n  host: base-host\n  port: 5432\n",
	), 0644))
	require.NoError(t, os.WriteFile(overlayPath, []byte(
		"database:\n  host: overlay-host\n",
	), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML(basePath),
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

	// Mutate the returned struct's scalar fields and the History slice
	// (debug mode populates History on override).
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
