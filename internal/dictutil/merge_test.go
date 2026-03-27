package dictutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name    string
		base    map[string]any
		overlay map[string]any
		want    map[string]any
	}{
		{
			name:    "empty maps",
			base:    map[string]any{},
			overlay: map[string]any{},
			want:    map[string]any{},
		},
		{
			name:    "overlay adds new keys",
			base:    map[string]any{"a": 1},
			overlay: map[string]any{"b": 2},
			want:    map[string]any{"a": 1, "b": 2},
		},
		{
			name:    "overlay replaces scalar",
			base:    map[string]any{"a": 1},
			overlay: map[string]any{"a": 2},
			want:    map[string]any{"a": 2},
		},
		{
			name: "nested maps are recursively merged",
			base: map[string]any{
				"db": map[string]any{"host": "localhost", "port": 5432},
			},
			overlay: map[string]any{
				"db": map[string]any{"host": "prod-db"},
			},
			want: map[string]any{
				"db": map[string]any{"host": "prod-db", "port": 5432},
			},
		},
		{
			name: "type mismatch falls back to replace",
			base: map[string]any{
				"db": map[string]any{"host": "localhost"},
			},
			overlay: map[string]any{
				"db": "string-value",
			},
			want: map[string]any{
				"db": "string-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeepMerge(tt.base, tt.overlay)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShallowMerge(t *testing.T) {
	base := map[string]any{
		"db": map[string]any{"host": "localhost", "port": 5432},
	}
	overlay := map[string]any{
		"db": map[string]any{"host": "prod-db"},
	}
	got := ShallowMerge(base, overlay)

	// Shallow merge replaces the entire "db" value.
	assert.Equal(t, map[string]any{"host": "prod-db"}, got["db"])
}

func TestGetNested(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}

	tests := []struct {
		path  string
		want  any
		found bool
	}{
		{"database.host", "localhost", true},
		{"database.port", 5432, true},
		{"debug", true, true},
		{"database.missing", nil, false},
		{"nonexistent", nil, false},
		{"database.host.deep", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := GetNested(data, tt.path)
			assert.Equal(t, tt.found, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetNested(t *testing.T) {
	data := make(map[string]any)
	require.NoError(t, SetNested(data, "database.host", "localhost"))
	require.NoError(t, SetNested(data, "database.port", 5432))
	require.NoError(t, SetNested(data, "debug", true))

	assert.Equal(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}, data)
}

func TestSetNested_IntermediateNotMap(t *testing.T) {
	data := map[string]any{"database": "scalar"}
	err := SetNested(data, "database.host", "localhost")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a map")
}

func TestHasNested(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{"host": "localhost"},
	}
	assert.True(t, HasNested(data, "database.host"))
	assert.False(t, HasNested(data, "database.port"))
}
