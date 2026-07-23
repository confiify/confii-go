package sourcetrack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// D06: GetSourceInfo defensive-copy contract.
//
// GetSourceInfo previously returned the live *SourceInfo pointer from the
// underlying map, which let callers (or a concurrent failed Reload that
// mutates state mid-load and then Restores from a snapshot) observe
// orphaned, mutated state through a stale pointer. The fix is to return a
// deep copy of the *SourceInfo (including its History slice). These tests
// pin that contract.
// =========================================================================

// TestTracker_GetSourceInfo_ReturnsDefensiveCopy verifies that mutating the
// *SourceInfo returned by GetSourceInfo does not leak back into tracker
// state observed by a subsequent GetSourceInfo call.
func TestTracker_GetSourceInfo_ReturnsDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "src", "yaml", "dev")

	first := tr.GetSourceInfo("foo")
	require.NotNil(t, first)
	require.Equal(t, 0, first.OverrideCount)
	originalSource := first.SourceFile

	// Mutate the returned struct's scalar fields.
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
	// And the two copies must be distinct pointers.
	assert.NotSame(t, first, second,
		"successive GetSourceInfo calls must return distinct copies")
}

// TestTracker_GetSourceInfo_HistorySliceDefensiveCopy verifies that the
// History slice is also deep-copied, so appending/overwriting it on the
// returned value does not corrupt the tracker's record.
func TestTracker_GetSourceInfo_HistorySliceDefensiveCopy(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "")
	tr.TrackValue("foo", 2, "s2", "yaml", "") // grows History to len 1
	tr.TrackValue("foo", 3, "s3", "yaml", "") // grows History to len 2

	first := tr.GetSourceInfo("foo")
	require.NotNil(t, first)
	require.Len(t, first.History, 2)

	// Mutate the History slice elements in place.
	first.History[0] = OverrideEntry{Value: "tampered", Source: "evil", LoaderType: "evil"}
	// And append further junk to the local copy.
	first.History = append(first.History, OverrideEntry{Value: "extra", Source: "evil"})

	second := tr.GetSourceInfo("foo")
	require.NotNil(t, second)
	require.Len(t, second.History, 2,
		"appending to returned History slice must not extend tracker's record")
	assert.Equal(t, 1, second.History[0].Value,
		"in-place mutation of returned History element must not leak")
	assert.Equal(t, "s1", second.History[0].Source,
		"in-place mutation of returned History element must not leak")
	// And the two history slice headers must reference distinct backing arrays.
	assert.NotSame(t, &first.History[0], &second.History[0],
		"returned History slices must not share backing storage")
}

// TestTracker_GetSourceInfo_NotFoundReturnsNil preserves the nil-on-miss
// contract — the defensive-copy wrapper must not start synthesising empty
// *SourceInfo values for unknown keys.
func TestTracker_GetSourceInfo_NotFoundReturnsNil(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("known", 1, "s", "yaml", "")

	assert.Nil(t, tr.GetSourceInfo("unknown"),
		"GetSourceInfo on unknown key must return nil")
	assert.NotNil(t, tr.GetSourceInfo("known"),
		"GetSourceInfo on known key must still return a value")
}

// TestTracker_GetSourceInfo_SurvivesRestoreOfStalePointer simulates the
// concrete D06 hazard: caller holds a *SourceInfo pointer, a Reload-style
// flow mutates the live record and then Restores from a snapshot. A
// pre-fix caller would observe the orphaned, mutated value through their
// stored pointer; a post-fix caller's pointer remains the pristine copy
// taken at GetSourceInfo time.
func TestTracker_GetSourceInfo_SurvivesRestoreOfStalePointer(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("foo", 1, "s1", "yaml", "dev")

	held := tr.GetSourceInfo("foo")
	require.NotNil(t, held)
	require.Equal(t, 1, held.Value)

	snap := tr.Snapshot()

	// Simulate a partial reload that mutates the live record...
	tr.TrackValue("foo", 2, "s2", "yaml", "dev")
	tr.TrackValue("foo", 3, "s3", "yaml", "dev")

	// ...and then rolls back via Restore.
	tr.Restore(snap)

	// The caller's pointer must still observe the value at GetSourceInfo
	// time — not the orphaned mid-load mutations.
	assert.Equal(t, 1, held.Value,
		"caller's held *SourceInfo must be insulated from later mutations")
	assert.Equal(t, 0, held.OverrideCount,
		"caller's held *SourceInfo must be insulated from later overrides")
}
