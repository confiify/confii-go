// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package configdecode

import (
	"testing"

	"github.com/confiify/confii-go/v2/internal/formatparse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapWithPositions_YAMLReportsKeyLines(t *testing.T) {
	doc := []byte("database:\n  host: localhost\n  port: 5432\napp:\n  name: svc\n")

	result, positions, err := MapWithPositions(doc, formatparse.FormatYAML)
	require.NoError(t, err)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])

	assert.Equal(t, 1, positions["database"])
	assert.Equal(t, 2, positions["database.host"])
	assert.Equal(t, 3, positions["database.port"])
	assert.Equal(t, 4, positions["app"])
	assert.Equal(t, 5, positions["app.name"])
}

func TestMapWithPositions_YAMLListValueReportsItsKeyLine(t *testing.T) {
	doc := []byte("app:\n  hosts:\n    - a\n    - b\n")

	_, positions, err := MapWithPositions(doc, formatparse.FormatYAML)
	require.NoError(t, err)
	assert.Equal(t, 2, positions["app.hosts"])
}

func TestMapWithPositions_MatchesMapForSameInput(t *testing.T) {
	doc := []byte("database:\n  host: localhost\n  port: 5432\n")

	want, err := Map(doc, formatparse.FormatYAML)
	require.NoError(t, err)
	got, _, err := MapWithPositions(doc, formatparse.FormatYAML)
	require.NoError(t, err)
	assert.Equal(t, want, got, "value decoding must be identical to Map")
}

func TestMapWithPositions_FormatsWithoutPositionsReturnNone(t *testing.T) {
	jsonDoc := []byte(`{"database":{"host":"localhost"}}`)
	result, positions, err := MapWithPositions(jsonDoc, formatparse.FormatJSON)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, positions, "JSON carries no position information")

	tomlDoc := []byte("[database]\nhost = \"localhost\"\n")
	result, positions, err = MapWithPositions(tomlDoc, formatparse.FormatTOML)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, positions, "TOML carries no position information")
}

func TestMapWithPositions_PropagatesDecodeErrors(t *testing.T) {
	_, _, err := MapWithPositions([]byte("a:\n  - b\n c: d\n"), formatparse.FormatYAML)
	require.Error(t, err)
}
