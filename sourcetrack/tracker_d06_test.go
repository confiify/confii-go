// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracker_GetSourceInfo_ReturnsDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "src", "yaml", "dev")

	first := tr.GetSourceInfo("foo")
	require.NotNil(t, first)
	require.Equal(t, 0, first.OverrideCount)
	originalSource := first.SourceFile

	first.OverrideCount = 999
	first.Value = "tampered"
	first.SourceFile = "tampered.yaml"

	second := tr.GetSourceInfo("foo")
	require.NotNil(t, second)
	assert.Equal(t, 0, second.OverrideCount,
		"OverrideCount mutation on returned copy must not leak into tracker")
	assert.Equal(t, 1, second.Value,
		"Value mutation on returned copy must not leak into tracker")
	assert.Equal(t, originalSource, second.SourceFile,
		"SourceFile mutation on returned copy must not leak into tracker")

	assert.NotSame(t, first, second,
		"successive GetSourceInfo calls must return distinct copies")
}

func TestTracker_GetSourceInfo_HistorySliceDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "")
	tr.TrackValue("foo", 2, "s2", "yaml", "")
	tr.TrackValue("foo", 3, "s3", "yaml", "")

	first := tr.GetSourceInfo("foo")
	require.NotNil(t, first)
	require.Len(t, first.History, 2)

	first.History[0] = OverrideEntry{Value: "tampered", Source: "evil", LoaderType: "evil"}

	first.History = append(first.History, OverrideEntry{Value: "extra", Source: "evil"})

	second := tr.GetSourceInfo("foo")
	require.NotNil(t, second)
	require.Len(t, second.History, 2,
		"appending to returned History slice must not extend tracker's record")
	assert.Equal(t, 1, second.History[0].Value,
		"in-place mutation of returned History element must not leak")
	assert.Equal(t, "s1", second.History[0].Source,
		"in-place mutation of returned History element must not leak")

	assert.NotSame(t, &first.History[0], &second.History[0],
		"returned History slices must not share backing storage")
}

func TestTracker_GetSourceInfo_NotFoundReturnsNil(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("known", 1, "s", "yaml", "")

	assert.Nil(t, tr.GetSourceInfo("unknown"),
		"GetSourceInfo on unknown key must return nil")
	assert.NotNil(t, tr.GetSourceInfo("known"),
		"GetSourceInfo on known key must still return a value")
}

func TestTracker_GetSourceInfo_SurvivesRestoreOfStalePointer(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "dev")

	held := tr.GetSourceInfo("foo")
	require.NotNil(t, held)
	require.Equal(t, 1, held.Value)

	snap := tr.Snapshot()

	tr.TrackValue("foo", 2, "s2", "yaml", "dev")
	tr.TrackValue("foo", 3, "s3", "yaml", "dev")

	tr.Restore(snap)

	assert.Equal(t, 1, held.Value,
		"caller's held *SourceInfo must be insulated from later mutations")
	assert.Equal(t, 0, held.OverrideCount,
		"caller's held *SourceInfo must be insulated from later overrides")
}
