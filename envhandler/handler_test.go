// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package envhandler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandler_Resolve_FlatConfig(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"host":  "localhost",
		"port":  5432,
		"debug": true,
	}
	got := h.Resolve(config, "production")
	assert.Equal(t, config, got)
}

func TestHandler_Resolve_WithDefaultAndEnv(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host":  "localhost",
			"port":  5432,
			"debug": true,
		},
		"production": map[string]any{
			"host":  "prod-db",
			"debug": false,
		},
	}
	got := h.Resolve(config, "production")
	assert.Equal(t, "prod-db", got["host"])
	assert.Equal(t, 5432, got["port"])
	assert.Equal(t, false, got["debug"])
}

func TestHandler_Resolve_DefaultOnly(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "localhost",
		},
	}
	got := h.Resolve(config, "staging")
	assert.Equal(t, config, got)
}

func TestHandler_Resolve_NoDefault(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"production": map[string]any{
			"host": "prod-db",
		},
	}
	got := h.Resolve(config, "production")
	assert.Equal(t, "prod-db", got["host"])
}

func TestHandler_Resolve_EmptyEnv(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "localhost",
		},
	}
	got := h.Resolve(config, "")
	assert.Equal(t, config, got)
}

func TestHandler_Resolve_DefaultPlusFlatSiblings_NoEnvMatch_PreservesSiblings(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"foo": 1,
		},
		"bar": 2,
		"baz": 3,
	}

	got := h.Resolve(config, "production")

	assert.Equal(t, 2, got["bar"])
	assert.Equal(t, 3, got["baz"])
	assert.Contains(t, got, "default")
	defMap, ok := got["default"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 1, defMap["foo"])
}

func TestHandler_Resolve_DefaultPlusMapSibling_AssertsEnvMode(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "default-host",
			"port": 5432,
		},
		"production": map[string]any{
			"host": "prod-host",
		},
	}

	got := h.Resolve(config, "staging")
	assert.Equal(t, "default-host", got["host"])
	assert.Equal(t, 5432, got["port"])

	assert.NotContains(t, got, "production")
}

func TestHandler_Resolve_DefaultOnly_NoSiblings_RemainsFlat(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	}

	got := h.Resolve(config, "production")
	assert.Equal(t, config, got)
}

func TestHandler_Resolve_ScalarDefault_FlatPassthrough(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": "en-US",
		"bar":     2,
		"baz":     3,
	}

	got := h.Resolve(config, "production")
	assert.Equal(t, config, got)
}
