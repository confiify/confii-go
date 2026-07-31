// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

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
