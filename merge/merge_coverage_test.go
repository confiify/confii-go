package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeAll_SingleConfig(t *testing.T) {
	m := NewDefault(true)
	cfg := map[string]any{"a": 1, "b": 2}
	got := MergeAll(m, cfg)
	assert.Equal(t, 1, got["a"])
	assert.Equal(t, 2, got["b"])
}

func TestMergeAll_NilFirstConfig(t *testing.T) {
	m := NewDefault(true)
	got := MergeAll(m, nil, map[string]any{"a": 1})
	assert.Equal(t, 1, got["a"])
}

func TestMergeAll_AllNil(t *testing.T) {
	m := NewDefault(true)
	got := MergeAll(m, nil, nil, nil)
	assert.NotNil(t, got)
}

func TestDefaultMerger_DeepMerge_NestedOverlap(t *testing.T) {
	m := NewDefault(true)
	base := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"a": 1,
				"b": 2,
			},
		},
	}
	overlay := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"b": 3,
				"c": 4,
			},
		},
	}
	got := m.Merge(base, overlay)
	l2 := got["level1"].(map[string]any)["level2"].(map[string]any)
	assert.Equal(t, 1, l2["a"])
	assert.Equal(t, 3, l2["b"])
	assert.Equal(t, 4, l2["c"])
}

func TestDefaultMerger_ShallowMerge_PreservesNonOverlapping(t *testing.T) {
	m := NewDefault(false)
	base := map[string]any{"a": 1, "b": 2}
	overlay := map[string]any{"c": 3}
	got := m.Merge(base, overlay)
	assert.Equal(t, 1, got["a"])
	assert.Equal(t, 2, got["b"])
	assert.Equal(t, 3, got["c"])
}

func TestAdvancedMerger_DeepMergeNonMapFallsToReplace(t *testing.T) {
	m := NewAdvanced(DeepMergeStrategy, nil)
	// When values are not maps, deep merge falls back to replace.
	base := map[string]any{"key": "base_string"}
	overlay := map[string]any{"key": "overlay_string"}
	got := m.Merge(base, overlay)
	assert.Equal(t, "overlay_string", got["key"])
}

func TestAdvancedMerger_UnionNonMapFallsToReplace(t *testing.T) {
	m := NewAdvanced(Union, nil)
	base := map[string]any{"key": 42}
	overlay := map[string]any{"key": 99}
	got := m.Merge(base, overlay)
	assert.Equal(t, 99, got["key"])
}

func TestAdvancedMerger_PrependNonList(t *testing.T) {
	m := NewAdvanced(Prepend, nil)
	base := map[string]any{"val": "a"}
	overlay := map[string]any{"val": "b"}
	got := m.Merge(base, overlay)
	assert.Equal(t, []any{"b", "a"}, got["val"])
}

func TestAdvancedMerger_NewKeyAddedRegardlessOfStrategy(t *testing.T) {
	strategies := []Strategy{Replace, DeepMergeStrategy, Append, Prepend, Union}
	for _, s := range strategies {
		m := NewAdvanced(s, nil)
		base := map[string]any{"existing": 1}
		overlay := map[string]any{"new_key": 2}
		got := m.Merge(base, overlay)
		assert.Equal(t, 2, got["new_key"], "strategy %d should add new keys", s)
		assert.Equal(t, 1, got["existing"], "strategy %d should keep existing keys", s)
	}
}

func TestAdvancedMerger_IntersectionNestedMaps(t *testing.T) {
	m := NewAdvanced(Intersection, nil)
	base := map[string]any{
		"shared":    map[string]any{"x": 1, "y": 2},
		"only_base": "val",
	}
	overlay := map[string]any{
		"shared":       map[string]any{"x": 1, "z": 3},
		"only_overlay": "val",
	}
	got := m.Merge(base, overlay)
	assert.NotContains(t, got, "only_base")
	assert.NotContains(t, got, "only_overlay")
	assert.Contains(t, got, "shared")
}

func TestAdvancedMerger_IntersectionEqualPrimitives(t *testing.T) {
	m := NewAdvanced(Intersection, nil)
	base := map[string]any{"a": "same", "b": "different"}
	overlay := map[string]any{"a": "same", "b": "other"}
	got := m.Merge(base, overlay)
	// "a" has equal values so it's kept.
	assert.Contains(t, got, "a")
}

func TestAdvancedMerger_EmptyMaps(t *testing.T) {
	m := NewAdvanced(DeepMergeStrategy, nil)

	// Empty base.
	got := m.Merge(map[string]any{}, map[string]any{"a": 1})
	assert.Equal(t, 1, got["a"])

	// Empty overlay.
	got = m.Merge(map[string]any{"a": 1}, map[string]any{})
	assert.Equal(t, 1, got["a"])

	// Both empty.
	got = m.Merge(map[string]any{}, map[string]any{})
	assert.Empty(t, got)
}

func TestAdvancedMerger_ResolveStrategyParentPath(t *testing.T) {
	m := NewAdvanced(DeepMergeStrategy, map[string]Strategy{
		"database":       Replace,
		"database.cache": Append,
	})

	// "database.host" should match parent "database" -> Replace.
	base := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
	}
	overlay := map[string]any{
		"database": map[string]any{
			"host": "prod-db",
		},
	}
	got := m.Merge(base, overlay)
	db := got["database"].(map[string]any)
	assert.Equal(t, "prod-db", db["host"])
	_, hasPort := db["port"]
	assert.False(t, hasPort) // replaced, not merged
}

func TestAdvancedMerger_ResolveStrategyMostSpecificParent(t *testing.T) {
	m := NewAdvanced(Replace, map[string]Strategy{
		"a":   DeepMergeStrategy,
		"a.b": Append,
	})

	// "a.b" has Append strategy. When merging "a", DeepMerge is used,
	// and for "a.b" the Append strategy applies.
	base := map[string]any{
		"a": map[string]any{
			"b": []any{"x"},
		},
	}
	overlay := map[string]any{
		"a": map[string]any{
			"b": []any{"y"},
		},
	}
	got := m.Merge(base, overlay)
	ab := got["a"].(map[string]any)["b"]
	// With Append strategy on "a.b", lists should be appended.
	assert.Equal(t, []any{"x", "y"}, ab)
}

func TestAdvancedMerger_DefaultUnknownStrategy(t *testing.T) {
	// Unknown strategy value falls to the default case which replaces.
	m := NewAdvanced(Strategy(99), nil)
	base := map[string]any{"key": "base"}
	overlay := map[string]any{"key": "overlay"}
	got := m.Merge(base, overlay)
	assert.Equal(t, "overlay", got["key"])
}

func TestIntersect_NonMapUnequalValues(t *testing.T) {
	result := intersect("a", "b")
	assert.Nil(t, result)
}

func TestIntersect_NonMapEqualValues(t *testing.T) {
	result := intersect("same", "same")
	assert.Equal(t, "same", result)
}

func TestIntersect_EmptyMaps(t *testing.T) {
	result := intersect(map[string]any{}, map[string]any{})
	assert.Nil(t, result) // no common keys -> nil
}

func TestToSlice_AlreadySlice(t *testing.T) {
	s := toSlice([]any{1, 2, 3})
	assert.Equal(t, []any{1, 2, 3}, s)
}

func TestToSlice_NonSlice(t *testing.T) {
	s := toSlice("hello")
	assert.Equal(t, []any{"hello"}, s)
}

// ===========================================================================
// AdvancedMerger with nested intersection where prefix != "" (lines 71-73)
// ===========================================================================

func TestAdvancedMerger_IntersectMaps_WithPrefix(t *testing.T) {
	m := NewAdvanced(Intersection, map[string]Strategy{
		"parent.child": Intersection,
	})

	base := map[string]any{
		"parent": map[string]any{
			"child": map[string]any{
				"shared":    "base_val",
				"only_base": "gone",
			},
		},
	}
	overlay := map[string]any{
		"parent": map[string]any{
			"child": map[string]any{
				"shared":       "base_val",
				"only_overlay": "new",
			},
		},
	}
	got := m.Merge(base, overlay)
	parent, ok := got["parent"].(map[string]any)
	require.True(t, ok)
	child, ok := parent["child"].(map[string]any)
	require.True(t, ok)
	// Intersection: only "shared" with equal values should be kept.
	assert.Equal(t, "base_val", child["shared"])
	_, hasOnlyBase := child["only_base"]
	assert.False(t, hasOnlyBase)
	_, hasOnlyOverlay := child["only_overlay"]
	assert.False(t, hasOnlyOverlay)
}

// ===========================================================================
// resolveStrategy parent path matching (lines 88-95)
// ===========================================================================

func TestAdvancedMerger_ResolveStrategy_DeepParentPath(t *testing.T) {
	m := NewAdvanced(DeepMergeStrategy, map[string]Strategy{
		"database": Replace,
	})

	// "database.connection.host" should resolve to parent "database" -> Replace.
	base := map[string]any{
		"database": map[string]any{
			"connection": map[string]any{
				"host": "localhost",
				"port": 5432,
			},
		},
	}
	overlay := map[string]any{
		"database": map[string]any{
			"connection": map[string]any{
				"host": "prod-db",
			},
		},
	}
	got := m.Merge(base, overlay)
	db := got["database"].(map[string]any)
	// With Replace strategy on "database", the entire "database" map is replaced.
	conn, ok := db["connection"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "prod-db", conn["host"])
	_, hasPort := conn["port"]
	assert.False(t, hasPort, "Replace should not keep port from base")
}

// ===========================================================================
// intersect with nested maps on both sides (lines 162-166)
// ===========================================================================

func TestIntersect_NestedMapsOnBothSides(t *testing.T) {
	base := map[string]any{
		"level1": map[string]any{
			"shared": map[string]any{
				"a": "same",
				"b": "diff_base",
			},
			"only_base": "gone",
		},
	}
	overlay := map[string]any{
		"level1": map[string]any{
			"shared": map[string]any{
				"a": "same",
				"b": "diff_overlay",
			},
			"only_overlay": "new",
		},
	}

	result := intersect(base, overlay)
	require.NotNil(t, result)
	rm := result.(map[string]any)
	level1 := rm["level1"].(map[string]any)
	shared := level1["shared"].(map[string]any)
	assert.Equal(t, "same", shared["a"])
	// "b" differs so it should not be in the intersection.
	_, hasB := shared["b"]
	assert.False(t, hasB)
}
