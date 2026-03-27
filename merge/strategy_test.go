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

func TestAdvancedMerger_Append_NonList(t *testing.T) {
	m := NewAdvanced(Append, nil)
	base := map[string]any{"val": "a"}
	overlay := map[string]any{"val": "b"}

	got := m.Merge(base, overlay)
	assert.Equal(t, []any{"a", "b"}, got["val"])
}

func TestAdvancedMerger_Intersection(t *testing.T) {
	m := NewAdvanced(Intersection, nil)
	base := map[string]any{"a": 1, "b": 2, "c": 3}
	overlay := map[string]any{"b": 2, "c": 99, "d": 4}

	got := m.Merge(base, overlay)
	assert.Equal(t, 2, got["b"])       // equal in both → kept
	assert.Nil(t, got["c"])            // different values → nil
	assert.NotContains(t, got, "a")    // only in base → excluded
	assert.NotContains(t, got, "d")    // only in overlay → excluded
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
	assert.Equal(t, false, app["debug"])   // overridden
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
