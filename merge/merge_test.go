package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultMerger_DeepMerge(t *testing.T) {
	m := NewDefault(true)

	base := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432},
		"debug":    false,
	}
	overlay := map[string]any{
		"database": map[string]any{"host": "prod-db"},
		"feature":  "new",
	}
	got := m.Merge(base, overlay)

	db := got["database"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	assert.Equal(t, 5432, db["port"]) // preserved from base
	assert.Equal(t, false, got["debug"])
	assert.Equal(t, "new", got["feature"])
}

func TestDefaultMerger_ShallowMerge(t *testing.T) {
	m := NewDefault(false)

	base := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432},
	}
	overlay := map[string]any{
		"database": map[string]any{"host": "prod-db"},
	}
	got := m.Merge(base, overlay)

	// Shallow merge replaces the entire map.
	db := got["database"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	_, hasPort := db["port"]
	assert.False(t, hasPort)
}

func TestMergeAll(t *testing.T) {
	m := NewDefault(true)

	configs := []map[string]any{
		{"a": 1, "b": 2},
		{"b": 3, "c": 4},
		{"c": 5, "d": 6},
	}

	got := MergeAll(m, configs...)
	assert.Equal(t, 1, got["a"])
	assert.Equal(t, 3, got["b"])
	assert.Equal(t, 5, got["c"])
	assert.Equal(t, 6, got["d"])
}

func TestMergeAll_Empty(t *testing.T) {
	m := NewDefault(true)
	got := MergeAll(m)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestMergeAll_NilConfigs(t *testing.T) {
	m := NewDefault(true)
	got := MergeAll(m, map[string]any{"a": 1}, nil, map[string]any{"b": 2})
	assert.Equal(t, 1, got["a"])
	assert.Equal(t, 2, got["b"])
}
