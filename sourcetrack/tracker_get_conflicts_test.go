// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracker_GetConflicts_ReturnsDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "dev")
	tr.TrackValue("foo", 2, "s2", "yaml", "dev")

	first := tr.GetConflicts()
	require.Contains(t, first, "foo")
	info := first["foo"]
	require.NotNil(t, info)
	originalSource := info.SourceFile
	originalValue := info.Value
	originalOverrideCount := info.OverrideCount

	info.SourceFile = "tampered.yaml"
	info.Value = "tampered"
	info.OverrideCount = 999

	delete(first, "foo")
	first["injected"] = &SourceInfo{Key: "injected"}

	second := tr.GetConflicts()
	require.Contains(t, second, "foo",
		"deletion from returned map must not remove tracker entries")
	assert.NotContains(t, second, "injected",
		"injection into returned map must not appear in tracker state")
	got := second["foo"]
	require.NotNil(t, got)
	assert.Equal(t, originalSource, got.SourceFile,
		"SourceFile mutation on returned copy must not leak into tracker")
	assert.Equal(t, originalValue, got.Value,
		"Value mutation on returned copy must not leak into tracker")
	assert.Equal(t, originalOverrideCount, got.OverrideCount,
		"OverrideCount mutation on returned copy must not leak into tracker")
	assert.NotSame(t, info, got,
		"successive GetConflicts calls must return distinct *SourceInfo copies")
}

func TestTracker_GetConflicts_HistorySliceDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "")
	tr.TrackValue("foo", 2, "s2", "yaml", "")
	tr.TrackValue("foo", 3, "s3", "yaml", "")

	first := tr.GetConflicts()
	require.Contains(t, first, "foo")
	info := first["foo"]
	require.NotNil(t, info)
	require.Len(t, info.History, 2)

	info.History[0] = OverrideEntry{Value: "tampered", Source: "evil", LoaderType: "evil"}
	info.History = append(info.History, OverrideEntry{Value: "extra", Source: "evil"})

	second := tr.GetConflicts()
	require.Contains(t, second, "foo")
	got := second["foo"]
	require.NotNil(t, got)
	require.Len(t, got.History, 2,
		"appending to returned History slice must not extend tracker's record")
	assert.Equal(t, 1, got.History[0].Value,
		"in-place mutation of returned History element must not leak")
	assert.Equal(t, "s1", got.History[0].Source,
		"in-place mutation of returned History element must not leak")
	assert.NotSame(t, &info.History[0], &got.History[0],
		"returned History slices must not share backing storage")
}

func TestTracker_GetConflicts_NotFoundReturnsEmpty(t *testing.T) {
	tr := NewTracker(false)
	conflicts := tr.GetConflicts()
	require.NotNil(t, conflicts,
		"GetConflicts on empty tracker must return non-nil map")
	assert.Len(t, conflicts, 0)

	tr.TrackValue("a", 1, "s1", "yaml", "")
	conflicts = tr.GetConflicts()
	require.NotNil(t, conflicts,
		"GetConflicts with no conflicts must return non-nil map")
	assert.Len(t, conflicts, 0,
		"GetConflicts with no conflicts must return zero-length map")
}

func TestTracker_GetConflicts_SurvivesRestoreOfStalePointer(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "dev")
	tr.TrackValue("foo", 2, "s2", "yaml", "dev")

	conflicts := tr.GetConflicts()
	require.Contains(t, conflicts, "foo")
	held := conflicts["foo"]
	require.NotNil(t, held)
	heldValue := held.Value
	heldOverrideCount := held.OverrideCount

	snap := tr.Snapshot()

	tr.TrackValue("foo", 99, "evil", "yaml", "dev")
	tr.TrackValue("foo", 100, "evil2", "yaml", "dev")

	tr.Restore(snap)

	assert.Equal(t, heldValue, held.Value,
		"caller's *SourceInfo from GetConflicts must be insulated from later mutations")
	assert.Equal(t, heldOverrideCount, held.OverrideCount,
		"caller's *SourceInfo from GetConflicts must be insulated from later overrides")
}

func TestTracker_GetOverrideHistory_ReturnsDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("k", "a", "s1", "yaml", "")
	tr.TrackValue("k", "b", "s2", "yaml", "")
	tr.TrackValue("k", "c", "s3", "yaml", "")

	first := tr.GetOverrideHistory("k")
	require.Len(t, first, 2)
	require.Equal(t, "a", first[0].Value)
	require.Equal(t, "s1", first[0].Source)

	first[0] = OverrideEntry{Value: "tampered", Source: "evil", LoaderType: "evil"}
	first[1] = OverrideEntry{Value: "tampered2", Source: "evil2", LoaderType: "evil"}

	second := tr.GetOverrideHistory("k")
	require.Len(t, second, 2)
	assert.Equal(t, "a", second[0].Value,
		"in-place mutation of returned history must not leak into tracker")
	assert.Equal(t, "s1", second[0].Source,
		"in-place mutation of returned history must not leak into tracker")
	assert.Equal(t, "b", second[1].Value,
		"in-place mutation of returned history must not leak into tracker")
	assert.NotSame(t, &first[0], &second[0],
		"returned history slices must not share backing storage")
}

func TestTracker_GetOverrideHistory_AppendDoesNotCorrupt(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("k", "a", "s1", "yaml", "")
	tr.TrackValue("k", "b", "s2", "yaml", "")

	first := tr.GetOverrideHistory("k")
	require.Len(t, first, 1)

	first = append(first, OverrideEntry{Value: "junk", Source: "evil"})
	first = append(first, OverrideEntry{Value: "junk2", Source: "evil"})
	require.Len(t, first, 3)

	tr.TrackValue("k", "c", "s3", "yaml", "")

	second := tr.GetOverrideHistory("k")
	require.Len(t, second, 2,
		"tracker history must be exactly the genuine override entries, not aliased through caller's append")
	assert.Equal(t, "a", second[0].Value)
	assert.Equal(t, "b", second[1].Value)
}
