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
	assert.Equal(t, 5432, got["port"])   // from default
	assert.Equal(t, false, got["debug"]) // overridden
}

func TestHandler_Resolve_DefaultOnly(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "localhost",
		},
	}
	got := h.Resolve(config, "staging")
	assert.Equal(t, "localhost", got["host"])
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
	assert.Equal(t, "localhost", got["host"])
}

// G18 regression: a top-level "default" key alongside flat scalar siblings
// must NOT trigger environment mode. The previous heuristic switched into
// env-mode purely on the presence of "default" and then dropped siblings
// when no matching env section existed. The fix requires an explicit env
// signal (named env present, or another map sibling); without one the
// config is returned verbatim so legitimate keys survive.
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

	// All three top-level keys must be preserved (no stripping). The
	// "default" key itself is also preserved because no env signal was
	// asserted, so the handler treats this as a flat config.
	assert.Equal(t, 2, got["bar"])
	assert.Equal(t, 3, got["baz"])
	assert.Contains(t, got, "default")
	defMap, ok := got["default"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 1, defMap["foo"])
}

// G18 positive case: when "default" is paired with at least one map sibling
// (a recognizable environment section), env-mode IS asserted even when the
// requested env is missing. This mirrors real environment-aware configs and
// preserves the existing "warn + fall back to defaults" behavior.
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

	// Requesting an env that doesn't exist: env-mode is still asserted
	// because "production" is a map sibling, so we fall back to defaults.
	got := h.Resolve(config, "staging")
	assert.Equal(t, "default-host", got["host"])
	assert.Equal(t, 5432, got["port"])
	// In env-mode the "production" sibling is consumed as a section, not
	// surfaced as a top-level key.
	assert.NotContains(t, got, "production")
}

// G18 boundary: when only the "default" key exists (no other siblings at
// all) the historical default-only shorthand is preserved — there are no
// siblings to drop, so returning the default map's contents is unambiguous
// and matches prior behavior.
func TestHandler_Resolve_DefaultOnly_NoSiblings_ReturnsDefaultContents(t *testing.T) {
	h := New(nil)
	config := map[string]any{
		"default": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	}

	got := h.Resolve(config, "production")
	assert.Equal(t, "localhost", got["host"])
	assert.Equal(t, 5432, got["port"])
}

// G18 boundary: a flat config that happens to contain a top-level scalar
// "default" (e.g. a default locale) must not be reinterpreted as env-mode.
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
