package sourcetrack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// F-Tracker-GetConflicts-Aliasing: GetConflicts defensive-copy contract.
//
// GetConflicts previously returned the live *SourceInfo pointers from
// the underlying t.sources map (and Config.GetConflicts re-exposed
// them). That meant a caller mutating any returned value — or a
// concurrent TrackValue/Restore — could observe or cause corrupt
// tracker state through the aliased pointer. The fix mirrors the
// Wave 9 D06 cloneSourceInfo treatment of GetSourceInfo: every
// *SourceInfo handed out is a deep copy, including its History slice.
//
// This file pins the contract for GetConflicts and the sibling
// read-method GetOverrideHistory (which also previously returned the
// live History slice).
// =========================================================================

// TestTracker_GetConflicts_ReturnsDefensiveCopy verifies that mutating
// the *SourceInfo returned via GetConflicts does not leak back into
// tracker state observed by a subsequent GetConflicts call.
func TestTracker_GetConflicts_ReturnsDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "dev")
	tr.TrackValue("foo", 2, "s2", "yaml", "dev") // OverrideCount → 1

	first := tr.GetConflicts()
	require.Contains(t, first, "foo")
	info := first["foo"]
	require.NotNil(t, info)
	originalSource := info.SourceFile
	originalValue := info.Value
	originalOverrideCount := info.OverrideCount

	// Mutate the returned struct's scalar fields.
	info.SourceFile = "tampered.yaml"
	info.Value = "tampered"
	info.OverrideCount = 999
	// Mutate the returned map directly.
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

// TestTracker_GetConflicts_HistorySliceDefensiveCopy verifies that the
// History slice is also deep-copied, so in-place mutation or append
// on the returned value does not corrupt the tracker's record.
func TestTracker_GetConflicts_HistorySliceDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "")
	tr.TrackValue("foo", 2, "s2", "yaml", "") // History len 1
	tr.TrackValue("foo", 3, "s3", "yaml", "") // History len 2

	first := tr.GetConflicts()
	require.Contains(t, first, "foo")
	info := first["foo"]
	require.NotNil(t, info)
	require.Len(t, info.History, 2)

	// Mutate History elements in place + append.
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

// TestTracker_GetConflicts_NotFoundReturnsEmpty verifies the empty
// contract: when no keys have been overridden, GetConflicts returns an
// empty (non-nil, zero-length) map — the documented behavior pinned by
// TestGetConflicts_None in tracker_coverage_test.go.
func TestTracker_GetConflicts_NotFoundReturnsEmpty(t *testing.T) {
	tr := NewTracker(false)
	conflicts := tr.GetConflicts()
	require.NotNil(t, conflicts,
		"GetConflicts on empty tracker must return non-nil map")
	assert.Len(t, conflicts, 0)

	tr.TrackValue("a", 1, "s1", "yaml", "") // not yet overridden
	conflicts = tr.GetConflicts()
	require.NotNil(t, conflicts,
		"GetConflicts with no conflicts must return non-nil map")
	assert.Len(t, conflicts, 0,
		"GetConflicts with no conflicts must return zero-length map")
}

// TestTracker_GetConflicts_SurvivesRestoreOfStalePointer pins the
// concrete D06-class hazard for GetConflicts: caller holds a
// *SourceInfo from GetConflicts, a Reload-style flow mutates the live
// record and then Restores from a snapshot. A pre-fix caller would
// observe the orphaned, mutated value through their stored pointer; a
// post-fix caller's pointer remains the pristine copy taken at
// GetConflicts time.
func TestTracker_GetConflicts_SurvivesRestoreOfStalePointer(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "dev")
	tr.TrackValue("foo", 2, "s2", "yaml", "dev") // OverrideCount → 1

	conflicts := tr.GetConflicts()
	require.Contains(t, conflicts, "foo")
	held := conflicts["foo"]
	require.NotNil(t, held)
	heldValue := held.Value
	heldOverrideCount := held.OverrideCount

	snap := tr.Snapshot()

	// Mutate the live record...
	tr.TrackValue("foo", 99, "evil", "yaml", "dev")
	tr.TrackValue("foo", 100, "evil2", "yaml", "dev")

	// ...and then roll back via Restore.
	tr.Restore(snap)

	assert.Equal(t, heldValue, held.Value,
		"caller's *SourceInfo from GetConflicts must be insulated from later mutations")
	assert.Equal(t, heldOverrideCount, held.OverrideCount,
		"caller's *SourceInfo from GetConflicts must be insulated from later overrides")
}

// =========================================================================
// Sibling read-method audit: GetOverrideHistory.
//
// Pre-fix, GetOverrideHistory returned info.History directly — a live
// slice into the tracker's *SourceInfo. The same aliasing hazard
// applies: a caller mutating the returned slice (or a concurrent
// TrackValue appending a new history entry) would corrupt the other
// side. The fix returns append([]OverrideEntry(nil), info.History...).
// =========================================================================

// TestTracker_GetOverrideHistory_ReturnsDefensiveCopy verifies that
// the slice returned by GetOverrideHistory does not share backing
// storage with the tracker's internal History slice.
func TestTracker_GetOverrideHistory_ReturnsDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("k", "a", "s1", "yaml", "")
	tr.TrackValue("k", "b", "s2", "yaml", "") // History len 1
	tr.TrackValue("k", "c", "s3", "yaml", "") // History len 2

	first := tr.GetOverrideHistory("k")
	require.Len(t, first, 2)
	require.Equal(t, "a", first[0].Value)
	require.Equal(t, "s1", first[0].Source)

	// Mutate elements in place.
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

// TestTracker_GetOverrideHistory_AppendDoesNotCorrupt verifies that
// appending to the returned slice does not corrupt the tracker's
// History (the slice header carries fresh capacity from the defensive
// copy, so future Trackvalue calls observe the original layout).
func TestTracker_GetOverrideHistory_AppendDoesNotCorrupt(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("k", "a", "s1", "yaml", "")
	tr.TrackValue("k", "b", "s2", "yaml", "") // History len 1

	first := tr.GetOverrideHistory("k")
	require.Len(t, first, 1)

	// Append junk to the returned slice.
	first = append(first, OverrideEntry{Value: "junk", Source: "evil"})
	first = append(first, OverrideEntry{Value: "junk2", Source: "evil"})
	require.Len(t, first, 3)

	// Drive a real override on the tracker.
	tr.TrackValue("k", "c", "s3", "yaml", "")

	second := tr.GetOverrideHistory("k")
	require.Len(t, second, 2,
		"tracker history must be exactly the genuine override entries, not aliased through caller's append")
	assert.Equal(t, "a", second[0].Value)
	assert.Equal(t, "b", second[1].Value)
}
