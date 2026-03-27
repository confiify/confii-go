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
	assert.Equal(t, 5432, got["port"])  // from default
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
