package diff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiff_AddedRemovedModified(t *testing.T) {
	c1 := map[string]any{"a": 1, "b": 2, "c": 3}
	c2 := map[string]any{"a": 1, "b": 99, "d": 4}

	diffs := Diff(c1, c2)
	require.Len(t, diffs, 3)

	byKey := make(map[string]ConfigDiff)
	for _, d := range diffs {
		byKey[d.Key] = d
	}

	assert.Equal(t, Modified, byKey["b"].Type)
	assert.Equal(t, 2, byKey["b"].OldValue)
	assert.Equal(t, 99, byKey["b"].NewValue)

	assert.Equal(t, Removed, byKey["c"].Type)
	assert.Equal(t, Added, byKey["d"].Type)
}

func TestDiff_NestedMaps(t *testing.T) {
	c1 := map[string]any{
		"db": map[string]any{"host": "localhost", "port": 5432},
	}
	c2 := map[string]any{
		"db": map[string]any{"host": "prod-db", "port": 5432},
	}

	diffs := Diff(c1, c2)
	require.Len(t, diffs, 1)
	assert.Equal(t, Modified, diffs[0].Type)
	assert.Len(t, diffs[0].NestedDiffs, 1)
	assert.Equal(t, "host", diffs[0].NestedDiffs[0].Key)
}

func TestDiff_NoDifferences(t *testing.T) {
	c := map[string]any{"a": 1, "b": "hello"}
	diffs := Diff(c, c)
	assert.Empty(t, diffs)
}

func TestSummary(t *testing.T) {
	diffs := []ConfigDiff{
		{Type: Added},
		{Type: Removed},
		{Type: Modified, NestedDiffs: []ConfigDiff{{Type: Modified}}},
	}
	s := Summary(diffs)
	assert.Equal(t, 4, s["total"])
	assert.Equal(t, 1, s["added"])
	assert.Equal(t, 1, s["removed"])
	assert.Equal(t, 2, s["modified"])
}

func TestDriftDetector(t *testing.T) {
	intended := map[string]any{"host": "prod-db", "port": 5432}
	actual := map[string]any{"host": "dev-db", "port": 5432}

	d := NewDriftDetector(intended)
	assert.True(t, d.HasDrift(actual))

	drifts := d.DetectDrift(actual)
	require.Len(t, drifts, 1)
	assert.Equal(t, "host", drifts[0].Key)
}

func TestToJSON(t *testing.T) {
	diffs := []ConfigDiff{{Key: "a", Type: Added, NewValue: 1, Path: "a"}}
	s, err := ToJSON(diffs)
	require.NoError(t, err)
	assert.Contains(t, s, "added")
}
