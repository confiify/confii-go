// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package envhandler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

	assert.Contains(t, envs, "production")
	assert.Contains(t, envs, "staging")
	assert.NotContains(t, envs, "default")
	assert.NotContains(t, envs, "simple_string")
}

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

	got := h.Resolve(config, "staging")
	assert.Equal(t, "default-host", got["host"])
	assert.Equal(t, 5432, got["port"])
	assert.Equal(t, false, got["debug"])
}

func TestHandler_Resolve_NoDefault_NoEnv(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"host":  "flat-host",
		"port":  3306,
		"debug": true,
	}

	got := h.Resolve(config, "production")
	assert.Equal(t, config, got)
}

func TestHandler_Resolve_EnvValueNotMap(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "default-host",
		},
		"production": "not-a-map",
	}

	got := h.Resolve(config, "production")

	assert.Equal(t, "default-host", got["host"])
}

func TestHandler_Resolve_DefaultValueNotMap(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default":    "not-a-map",
		"production": map[string]any{"host": "prod-host"},
	}

	got := h.Resolve(config, "production")

	assert.Equal(t, "prod-host", got["host"])
}

func TestHandler_Resolve_EmptyEnvString_WithDefault(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "default-host",
		},
	}

	got := h.Resolve(config, "")
	assert.Equal(t, config, got)
}

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

	assert.Equal(t, 5432, db["port"])
	assert.Equal(t, "defaultdb", db["name"])
}

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

	assert.Equal(t, "hello", got["string_val"])
	assert.Equal(t, 42, got["int_val"])
	assert.Equal(t, true, got["bool_val"])
	assert.Equal(t, 3.14, got["float_val"])
}

func TestNew_WithNonNilLogger(t *testing.T) {
	h := New(nil)
	assert.NotNil(t, h.logger)
}
