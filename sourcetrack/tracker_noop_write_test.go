// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A later layer that restates the value an earlier layer already supplied has
// not overridden anything. OverrideCount, GetConflicts, and the override
// history all describe overrides, so none of them may report such a write.

func TestTrackValue_IdenticalValueIsNotAnOverride(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("app.port", 8080, "base.yaml", "yaml", "dev")
	tr.TrackValue("app.port", 8080, "env.yaml", "yaml", "dev")

	info := tr.GetSourceInfo("app.port")
	require.NotNil(t, info)
	assert.Equal(t, 0, info.OverrideCount,
		"restating an identical value must not count as an override")
	assert.Empty(t, info.History,
		"an unchanged value must not produce an override history entry")
}

func TestTrackValue_IdenticalValueStillUpdatesProvenance(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("app.port", 8080, "base.yaml", "yaml", "dev")
	tr.TrackValue("app.port", 8080, "env.yaml", "toml", "dev")

	info := tr.GetSourceInfo("app.port")
	require.NotNil(t, info)
	assert.Equal(t, "env.yaml", info.SourceFile,
		"the last layer to supply the effective value is its source")
	assert.Equal(t, "toml", info.LoaderType)
	assert.Equal(t, 8080, info.Value)
}

func TestTrackValue_IdenticalCompositeValueIsNotAnOverride(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("app.hosts", []any{"a", "b"}, "base.yaml", "yaml", "")
	tr.TrackValue("app.hosts", []any{"a", "b"}, "env.yaml", "yaml", "")

	info := tr.GetSourceInfo("app.hosts")
	require.NotNil(t, info)
	assert.Equal(t, 0, info.OverrideCount,
		"equality must be by value, not by identity")
}

func TestTrackValue_ChangeAfterIdenticalWriteCountsOnce(t *testing.T) {
	tr := NewTracker(true)
	tr.TrackValue("app.port", 8080, "base.yaml", "yaml", "")
	tr.TrackValue("app.port", 8080, "same.yaml", "yaml", "")
	tr.TrackValue("app.port", 9090, "real.yaml", "yaml", "")

	info := tr.GetSourceInfo("app.port")
	require.NotNil(t, info)
	assert.Equal(t, 1, info.OverrideCount)
	require.Len(t, info.History, 1)
	assert.Equal(t, 8080, info.History[0].Value)
	assert.Equal(t, "same.yaml", info.History[0].Source,
		"history records the source that supplied the value being replaced")
}

func TestGetConflicts_ExcludesUnchangedValues(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("unchanged", 1, "base.yaml", "yaml", "")
	tr.TrackValue("unchanged", 1, "env.yaml", "yaml", "")
	tr.TrackValue("changed", 1, "base.yaml", "yaml", "")
	tr.TrackValue("changed", 2, "env.yaml", "yaml", "")

	conflicts := tr.GetConflicts()
	assert.NotContains(t, conflicts, "unchanged",
		"a key whose value never changed is not a conflict")
	assert.Contains(t, conflicts, "changed")
	assert.Len(t, conflicts, 1)
}

func TestGetSourceStatistics_ExcludesUnchangedValues(t *testing.T) {
	tr := NewTracker(false)
	tr.TrackValue("a", 1, "base.yaml", "yaml", "")
	tr.TrackValue("a", 1, "same.yaml", "yaml", "")
	tr.TrackValue("b", 2, "base.yaml", "yaml", "")
	tr.TrackValue("b", 3, "real.yaml", "yaml", "")

	stats := tr.GetSourceStatistics()
	assert.Equal(t, 1, stats["total_overrides"],
		"total_overrides counts real overrides only")
}
