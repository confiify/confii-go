// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackConfigWithPositions_RecordsLineNumbers(t *testing.T) {
	tr := NewTracker(false)
	config := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432},
		"app":      map[string]any{"name": "svc"},
	}
	positions := map[string]int{
		"database.host": 2,
		"database.port": 3,
		"app.name":      5,
	}

	tr.TrackConfigWithPositions(config, "app.yaml", "yaml", "dev", "", positions)

	require.NotNil(t, tr.GetSourceInfo("database.host"))
	assert.Equal(t, 2, tr.GetSourceInfo("database.host").LineNumber)
	assert.Equal(t, 3, tr.GetSourceInfo("database.port").LineNumber)
	assert.Equal(t, 5, tr.GetSourceInfo("app.name").LineNumber)
}

func TestTrackConfigWithPositions_UnknownKeyStaysZero(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackConfigWithPositions(
		map[string]any{"a": 1, "b": 2}, "app.yaml", "yaml", "", "",
		map[string]int{"a": 7},
	)

	assert.Equal(t, 7, tr.GetSourceInfo("a").LineNumber)
	assert.Equal(t, 0, tr.GetSourceInfo("b").LineNumber,
		"a key with no reported position must stay zero, not borrow another's")
}

func TestTrackConfigWithPositions_NilPositionsBehavesLikeTrackConfig(t *testing.T) {
	withNil := NewTracker(false)
	withNil.TrackConfigWithPositions(map[string]any{"a": 1}, "app.yaml", "yaml", "dev", "", nil)

	plain := NewTracker(false)
	plain.TrackConfig(map[string]any{"a": 1}, "app.yaml", "yaml", "dev", "")

	got, want := withNil.GetSourceInfo("a"), plain.GetSourceInfo("a")
	require.NotNil(t, got)
	require.NotNil(t, want)
	assert.Equal(t, want.Value, got.Value)
	assert.Equal(t, want.SourceFile, got.SourceFile)
	assert.Equal(t, 0, got.LineNumber)
}

func TestTrackConfigWithPositions_LaterSourceUpdatesLineNumber(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackConfigWithPositions(map[string]any{"a": 1}, "base.yaml", "yaml", "", "",
		map[string]int{"a": 3})
	tr.TrackConfigWithPositions(map[string]any{"a": 2}, "env.yaml", "yaml", "", "",
		map[string]int{"a": 9})

	info := tr.GetSourceInfo("a")
	require.NotNil(t, info)
	assert.Equal(t, 9, info.LineNumber,
		"line number tracks the source that supplied the effective value")
	assert.Equal(t, 1, info.OverrideCount)
}

func TestTrackConfigWithPositions_UnchangedValueStillUpdatesLineNumber(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackConfigWithPositions(map[string]any{"a": 1}, "base.yaml", "yaml", "", "",
		map[string]int{"a": 3})
	tr.TrackConfigWithPositions(map[string]any{"a": 1}, "env.yaml", "yaml", "", "",
		map[string]int{"a": 9})

	info := tr.GetSourceInfo("a")
	require.NotNil(t, info)
	assert.Equal(t, 9, info.LineNumber,
		"provenance, including the line, follows the newer source")
	assert.Equal(t, 0, info.OverrideCount,
		"an unchanged value is still not an override")
}
