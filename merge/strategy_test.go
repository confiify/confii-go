// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdvancedMerger_Replace(t *testing.T) {
	m := NewAdvanced(Replace, nil)
	base := map[string]any{"a": 1, "b": map[string]any{"x": 1}}
	overlay := map[string]any{"a": 2, "b": map[string]any{"y": 2}}

	got := m.Merge(base, overlay)
	assert.Equal(t, 2, got["a"])
	assert.Equal(t, map[string]any{"y": 2}, got["b"])
}

func TestStrategyReplace_DiscardsBaseKeys(t *testing.T) {
	m := NewAdvanced(Replace, nil)
	base := map[string]any{
		"a": 1,
		"b": 2,
	}
	overlay := map[string]any{
		"c": 3,
	}

	got := m.Merge(base, overlay)
	assert.Equal(t, 3, got["c"])
	assert.NotContains(t, got, "a")
	assert.NotContains(t, got, "b")
	assert.Len(t, got, 1)
}

func TestAdvancedMerger_DeepMerge(t *testing.T) {
	m := NewAdvanced(DeepMergeStrategy, nil)
	base := map[string]any{"db": map[string]any{"host": "localhost", "port": 5432}}
	overlay := map[string]any{"db": map[string]any{"host": "prod-db"}}

	got := m.Merge(base, overlay)
	db := got["db"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	assert.Equal(t, 5432, db["port"])
}

func TestAdvancedMerger_Append(t *testing.T) {
	m := NewAdvanced(Append, nil)
	base := map[string]any{"items": []any{"a", "b"}}
	overlay := map[string]any{"items": []any{"c"}}

	got := m.Merge(base, overlay)
	assert.Equal(t, []any{"a", "b", "c"}, got["items"])
}

func TestAdvancedMerger_Prepend(t *testing.T) {
	m := NewAdvanced(Prepend, nil)
	base := map[string]any{"items": []any{"a", "b"}}
	overlay := map[string]any{"items": []any{"c"}}

	got := m.Merge(base, overlay)
	assert.Equal(t, []any{"c", "a", "b"}, got["items"])
}

func TestStrategyAppend_TypeMismatch_DocumentedBehavior(t *testing.T) {
	m := NewAdvanced(Append, nil)

	got := m.Merge(map[string]any{"val": "a"}, map[string]any{"val": "b"})
	assert.Equal(t, []any{"a", "b"}, got["val"])

	got = m.Merge(map[string]any{"val": "a"}, map[string]any{"val": []any{"x", "y"}})
	assert.Equal(t, []any{"a", "x", "y"}, got["val"])

	got = m.Merge(map[string]any{"val": []any{"x", "y"}}, map[string]any{"val": "a"})
	assert.Equal(t, []any{"x", "y", "a"}, got["val"])
}

func TestStrategyPrepend_TypeMismatch_DocumentedBehavior(t *testing.T) {
	m := NewAdvanced(Prepend, nil)

	got := m.Merge(map[string]any{"val": "a"}, map[string]any{"val": "b"})
	assert.Equal(t, []any{"b", "a"}, got["val"])

	got = m.Merge(map[string]any{"val": "a"}, map[string]any{"val": []any{"x", "y"}})
	assert.Equal(t, []any{"x", "y", "a"}, got["val"])

	got = m.Merge(map[string]any{"val": []any{"x", "y"}}, map[string]any{"val": "a"})
	assert.Equal(t, []any{"a", "x", "y"}, got["val"])
}

func TestAdvancedMerger_Intersection(t *testing.T) {
	m := NewAdvanced(Intersection, nil)
	base := map[string]any{"a": 1, "b": 2, "c": 3}
	overlay := map[string]any{"b": 2, "c": 99, "d": 4}

	got := m.Merge(base, overlay)
	assert.Equal(t, 2, got["b"])
	assert.NotContains(t, got, "c")
	assert.NotContains(t, got, "a")
	assert.NotContains(t, got, "d")
}

func TestStrategyIntersection_OmitsUnequalScalars(t *testing.T) {
	m := NewAdvanced(Intersection, nil)
	base := map[string]any{"a": 1}
	overlay := map[string]any{"a": 2}

	got := m.Merge(base, overlay)
	assert.NotContains(t, got, "a")
	assert.Empty(t, got)
}

func TestStrategyIntersection_KeepsEqualScalars(t *testing.T) {
	m := NewAdvanced(Intersection, nil)
	base := map[string]any{"a": 1}
	overlay := map[string]any{"a": 1}

	got := m.Merge(base, overlay)
	assert.Equal(t, 1, got["a"])
	assert.Len(t, got, 1)
}

func TestAdvancedMerger_Union(t *testing.T) {
	m := NewAdvanced(Union, nil)
	base := map[string]any{"a": 1, "shared": map[string]any{"x": 1}}
	overlay := map[string]any{"b": 2, "shared": map[string]any{"y": 2}}

	got := m.Merge(base, overlay)
	assert.Equal(t, 1, got["a"])
	assert.Equal(t, 2, got["b"])
	shared := got["shared"].(map[string]any)
	assert.Equal(t, 1, shared["x"])
	assert.Equal(t, 2, shared["y"])
}

func TestStrategyUnion_RespectsNestedPathStrategies(t *testing.T) {
	m := NewAdvanced(Union, map[string]Strategy{
		"nested": Replace,
	})

	base := map[string]any{
		"top": "base_value",
		"nested": map[string]any{
			"keep_in_base": "base",
			"shared":       "base",
		},
	}
	overlay := map[string]any{
		"top": "overlay_value",
		"nested": map[string]any{
			"shared":         "overlay",
			"new_in_overlay": "overlay",
		},
	}

	got := m.Merge(base, overlay)

	assert.Equal(t, "overlay_value", got["top"])

	nested, ok := got["nested"].(map[string]any)
	assert.True(t, ok, "nested should be a map")
	assert.Equal(t, "overlay", nested["shared"])
	assert.Equal(t, "overlay", nested["new_in_overlay"])
	assert.NotContains(t, nested, "keep_in_base", "Replace should drop base-only keys")
}

func TestAdvancedMerger_PerPathOverride(t *testing.T) {
	m := NewAdvanced(DeepMergeStrategy, map[string]Strategy{
		"database": Replace,
		"features": Append,
	})

	base := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432},
		"features": []any{"auth"},
		"app":      map[string]any{"name": "myapp", "debug": true},
	}
	overlay := map[string]any{
		"database": map[string]any{"host": "prod-db"},
		"features": []any{"logging"},
		"app":      map[string]any{"debug": false},
	}

	got := m.Merge(base, overlay)

	db := got["database"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	_, hasPort := db["port"]
	assert.False(t, hasPort)

	assert.Equal(t, []any{"auth", "logging"}, got["features"])

	app := got["app"].(map[string]any)
	assert.Equal(t, "myapp", app["name"])
	assert.Equal(t, false, app["debug"])
}

func TestAdvancedMerger_ParentPathMatch(t *testing.T) {

	m := NewAdvanced(DeepMergeStrategy, map[string]Strategy{
		"database": Replace,
	})

	base := map[string]any{
		"database": map[string]any{
			"primary": map[string]any{"host": "localhost", "port": 5432},
		},
	}
	overlay := map[string]any{
		"database": map[string]any{
			"primary": map[string]any{"host": "prod-db"},
		},
	}

	got := m.Merge(base, overlay)
	db := got["database"].(map[string]any)
	primary := db["primary"].(map[string]any)
	assert.Equal(t, "prod-db", primary["host"])
	_, hasPort := primary["port"]
	assert.False(t, hasPort)
}
