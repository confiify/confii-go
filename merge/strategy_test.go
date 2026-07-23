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
	assert.Equal(t, map[string]any{"y": 2}, got["b"]) // replaced entirely
}

// TestStrategyReplace_DiscardsBaseKeys pins the corrected Replace
// semantic: when Replace governs a map level, base-only keys are
// dropped entirely. The previous implementation iterated overlay keys
// over a base copy and silently kept base-only entries.
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
	assert.Equal(t, 5432, db["port"]) // preserved
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

// TestStrategyAppend_TypeMismatch_DocumentedBehavior pins the documented
// scalar-to-list coercion used by Append.
func TestStrategyAppend_TypeMismatch_DocumentedBehavior(t *testing.T) {
	m := NewAdvanced(Append, nil)

	got := m.Merge(map[string]any{"val": "a"}, map[string]any{"val": "b"})
	assert.Equal(t, []any{"a", "b"}, got["val"])

	got = m.Merge(map[string]any{"val": "a"}, map[string]any{"val": []any{"x", "y"}})
	assert.Equal(t, []any{"a", "x", "y"}, got["val"])

	got = m.Merge(map[string]any{"val": []any{"x", "y"}}, map[string]any{"val": "a"})
	assert.Equal(t, []any{"x", "y", "a"}, got["val"])
}

// TestStrategyPrepend_TypeMismatch_DocumentedBehavior pins the documented
// scalar-to-list coercion used by Prepend.
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
	assert.Equal(t, 2, got["b"])    // equal in both → kept
	assert.NotContains(t, got, "c") // different values → omitted entirely
	assert.NotContains(t, got, "a") // only in base → excluded
	assert.NotContains(t, got, "d") // only in overlay → excluded
}

// TestStrategyIntersection_OmitsUnequalScalars pins the corrected
// Intersection semantic: when a key is shared but the scalar values
// differ, the key is omitted entirely. Previously the merger emitted
// {a: nil}, polluting the result with sentinel values that callers
// could not distinguish from a legitimate nil configuration value.
func TestStrategyIntersection_OmitsUnequalScalars(t *testing.T) {
	m := NewAdvanced(Intersection, nil)
	base := map[string]any{"a": 1}
	overlay := map[string]any{"a": 2}

	got := m.Merge(base, overlay)
	assert.NotContains(t, got, "a")
	assert.Empty(t, got)
}

// TestStrategyIntersection_KeepsEqualScalars confirms the inverse of
// the omission rule: shared keys with equal scalar values are kept.
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

// TestStrategyUnion_RespectsNestedPathStrategies verifies that the
// Union strategy now routes recursive merging through mergeAt so
// per-path strategy overrides on nested keys take effect. Previously
// Union called dictutil.DeepMerge directly, silently bypassing the
// StrategyMap for everything beneath the Union node.
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
		"top": "overlay_value", // Union: overlay wins on direct conflict
		"nested": map[string]any{
			"shared":         "overlay",
			"new_in_overlay": "overlay",
		},
	}

	got := m.Merge(base, overlay)

	// Top-level Union semantics: base-only "a" preserved, overlay
	// overrides on shared scalar.
	assert.Equal(t, "overlay_value", got["top"])

	// "nested" was governed by Replace per-path: base-only key must
	// be discarded, overlay map fully wins. With the old Union
	// implementation this assertion would fail because dictutil.DeepMerge
	// never consulted the strategy map.
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

	// database: Replace strategy.
	db := got["database"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	_, hasPort := db["port"]
	assert.False(t, hasPort) // replaced entirely

	// features: Append strategy.
	assert.Equal(t, []any{"auth", "logging"}, got["features"])

	// app: DeepMerge (default).
	app := got["app"].(map[string]any)
	assert.Equal(t, "myapp", app["name"]) // preserved
	assert.Equal(t, false, app["debug"])  // overridden
}

func TestAdvancedMerger_ParentPathMatch(t *testing.T) {
	// "database" strategy applies to "database.host" too.
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
	assert.False(t, hasPort) // replaced by parent strategy
}
