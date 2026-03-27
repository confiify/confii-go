package envhandler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// availableEnvs
// ---------------------------------------------------------------------------

func TestHandler_AvailableEnvs(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "localhost",
		},
		"production": map[string]any{
			"host": "prod-db",
		},
		"staging": map[string]any{
			"host": "staging-db",
		},
		"simple_string": "not-a-map",
	}

	envs := h.availableEnvs(config)
	// Should include production and staging but NOT default or simple_string.
	assert.Contains(t, envs, "production")
	assert.Contains(t, envs, "staging")
	assert.NotContains(t, envs, "default")
	assert.NotContains(t, envs, "simple_string")
}

// ---------------------------------------------------------------------------
// Missing environment with default section (warns and falls back)
// ---------------------------------------------------------------------------

func TestHandler_Resolve_MissingEnv_FallsBackToDefault(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host":  "default-host",
			"port":  5432,
			"debug": false,
		},
		"production": map[string]any{
			"host": "prod-host",
		},
	}

	// Request "staging" which doesn't exist -- should warn and return defaults.
	got := h.Resolve(config, "staging")
	assert.Equal(t, "default-host", got["host"])
	assert.Equal(t, 5432, got["port"])
	assert.Equal(t, false, got["debug"])
}

// ---------------------------------------------------------------------------
// No default section, flat config passthrough
// ---------------------------------------------------------------------------

func TestHandler_Resolve_NoDefault_NoEnv(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"host":  "flat-host",
		"port":  3306,
		"debug": true,
	}

	// No "default" key and no matching env key -- pass through as-is.
	got := h.Resolve(config, "production")
	assert.Equal(t, config, got)
}

// ---------------------------------------------------------------------------
// Has env key but value is not a map
// ---------------------------------------------------------------------------

func TestHandler_Resolve_EnvValueNotMap(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "default-host",
		},
		"production": "not-a-map",
	}

	got := h.Resolve(config, "production")
	// Should fall back to base (default) since env section is not a map.
	assert.Equal(t, "default-host", got["host"])
}

// ---------------------------------------------------------------------------
// Default value is not a map
// ---------------------------------------------------------------------------

func TestHandler_Resolve_DefaultValueNotMap(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default":    "not-a-map",
		"production": map[string]any{"host": "prod-host"},
	}

	got := h.Resolve(config, "production")
	// base is empty map since default is not a map.
	// Only production values should be present.
	assert.Equal(t, "prod-host", got["host"])
}

// ---------------------------------------------------------------------------
// Empty env string with default section
// ---------------------------------------------------------------------------

func TestHandler_Resolve_EmptyEnvString_WithDefault(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "default-host",
		},
	}

	// Empty env string: no env key match, should return defaults.
	// But because env="" and !hasEnv (config[""] doesn't exist), no warning is logged.
	got := h.Resolve(config, "")
	assert.Equal(t, "default-host", got["host"])
}

// ---------------------------------------------------------------------------
// Deep merge of env into default
// ---------------------------------------------------------------------------

func TestHandler_Resolve_DeepMerge(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"database": map[string]any{
				"host": "default-host",
				"port": 5432,
				"name": "defaultdb",
			},
		},
		"production": map[string]any{
			"database": map[string]any{
				"host": "prod-host",
			},
		},
	}

	got := h.Resolve(config, "production")
	db, ok := got["database"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "prod-host", db["host"])
	// port and name from default should be preserved via deep merge.
	assert.Equal(t, 5432, db["port"])
	assert.Equal(t, "defaultdb", db["name"])
}

// ---------------------------------------------------------------------------
// Env key exists but env value is empty map
// ---------------------------------------------------------------------------

func TestHandler_Resolve_EnvEmptyMap(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "default-host",
		},
		"production": map[string]any{},
	}

	got := h.Resolve(config, "production")
	assert.Equal(t, "default-host", got["host"])
}

// ---------------------------------------------------------------------------
// No env key, no default, mixed types
// ---------------------------------------------------------------------------

func TestHandler_Resolve_FlatConfig_MixedTypes(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"string_val": "hello",
		"int_val":    42,
		"bool_val":   true,
		"float_val":  3.14,
		"list_val":   []any{1, 2, 3},
	}

	got := h.Resolve(config, "any_env")
	// All values should pass through unchanged.
	assert.Equal(t, "hello", got["string_val"])
	assert.Equal(t, 42, got["int_val"])
	assert.Equal(t, true, got["bool_val"])
	assert.Equal(t, 3.14, got["float_val"])
}

// ---------------------------------------------------------------------------
// New with non-nil logger
// ---------------------------------------------------------------------------

func TestNew_WithNonNilLogger(t *testing.T) {
	h := New(nil)
	assert.NotNil(t, h.logger)
}
