package dictutil

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlatten(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}
	got := Flatten(data)
	assert.Equal(t, map[string]any{
		"database.host": "localhost",
		"database.port": 5432,
		"debug":         true,
	}, got)
}

func TestFlatKeys(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}
	keys := FlatKeys(data)
	sort.Strings(keys)
	assert.Equal(t, []string{"database.host", "database.port", "debug"}, keys)
}

func TestFlatKeysWithPrefix(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}
	keys := FlatKeysWithPrefix(data, "database")
	sort.Strings(keys)
	assert.Equal(t, []string{"host", "port"}, keys)
}

func TestFlatKeysWithPrefix_EmptyPrefix(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}
	keys := FlatKeysWithPrefix(data, "")
	sort.Strings(keys)
	// With empty prefix, all keys should be returned.
	assert.Equal(t, []string{"database.host", "database.port", "debug"}, keys)
}

func TestUnflatten(t *testing.T) {
	flat := map[string]any{
		"database.host": "localhost",
		"database.port": 5432,
		"debug":         true,
	}
	got := Unflatten(flat)
	assert.Equal(t, map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
		},
		"debug": true,
	}, got)
}
