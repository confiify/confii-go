// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package compose

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComposer_VisitedResetBetweenCalls confirms that the cycle-detection
// "visited" set is per-call, not per-Composer. Calling Compose twice on the
// same Composer instance with the same include should produce the same merged
// output both times. Under the previous (broken) behavior, the second call
// would silently skip the already-visited include.
func TestComposer_VisitedResetBetweenCalls(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "shared.yaml"),
		[]byte("shared_key: shared_val\nnested:\n  inner: deep\n"),
		0644,
	))

	c := New(dir)

	makeConfig := func() map[string]any {
		return map[string]any{
			"_include": []any{"shared.yaml"},
			"top":      "outer",
		}
	}

	// First call.
	first, err := c.Compose(makeConfig(), filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "shared_val", first["shared_key"], "first call must include shared file")
	assert.Equal(t, "outer", first["top"])
	nested, ok := first["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deep", nested["inner"])

	// Second call on the same Composer must produce the same result. If the
	// "visited" map were shared across calls, "shared.yaml" would be skipped
	// here and shared_key/nested would be missing.
	second, err := c.Compose(makeConfig(), filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "shared_val", second["shared_key"], "second call must re-include shared file (visited must reset between calls)")
	assert.Equal(t, "outer", second["top"])
	nested2, ok := second["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deep", nested2["inner"])

	// Third call after deliberately invoking many composes, simulating the
	// reload-loop pattern that previously regressed.
	for i := 0; i < 5; i++ {
		out, err := c.Compose(makeConfig(), filepath.Join(dir, "main.yaml"))
		require.NoError(t, err)
		assert.Equal(t, "shared_val", out["shared_key"], "iteration %d must re-include shared file", i)
	}
}

// TestComposer_CycleDetectionIntactWithinSingleCall pins down the
// cycle-detection guarantee. Within a single Compose call, mutually
// recursive includes must terminate (no infinite recursion or stack
// blow-up) and must surface the merged keys from each file exactly once.
func TestComposer_CycleDetectionIntactWithinSingleCall(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "a.yaml"),
		[]byte("_include:\n  - b.yaml\nfrom_a: true\n"),
		0644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "b.yaml"),
		[]byte("_include:\n  - a.yaml\nfrom_b: true\n"),
		0644,
	))

	config := map[string]any{
		"_include": []any{"a.yaml"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err, "cycle must terminate cleanly")
	assert.Equal(t, true, result["from_a"])
	assert.Equal(t, true, result["from_b"])
	_, hasInclude := result["_include"]
	assert.False(t, hasInclude, "_include directive should be stripped after composition")
}

// recordingMerger captures Merge calls so tests can assert which merger was
// used by the composer.
type recordingMerger struct {
	mu        sync.Mutex
	callCount int
	// fallback delegates the actual combine so the resulting test data is
	// still meaningful; we only care that the recordingMerger was invoked.
	fallback Merger
}

func newRecordingMerger(fallback Merger) *recordingMerger {
	return &recordingMerger{fallback: fallback}
}

func (r *recordingMerger) Merge(base, overlay map[string]any) map[string]any {
	r.mu.Lock()
	r.callCount++
	r.mu.Unlock()
	return r.fallback.Merge(base, overlay)
}

func (r *recordingMerger) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callCount
}

// TestComposer_UsesConfiguredMerger verifies that when a custom Merger is
// passed via WithMerger, the composer routes _include and _defaults merges
// through it rather than calling dictutil.DeepMerge directly.
func TestComposer_UsesConfiguredMerger(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "inc.yaml"),
		[]byte("included_key: included_val\n"),
		0644,
	))

	rec := newRecordingMerger(deepMerger{})
	c := New(dir, WithMerger(rec))

	config := map[string]any{
		"_defaults": []any{"default_key: default_val"},
		"_include":  []any{"inc.yaml"},
		"own":       "own_val",
	}

	result, err := c.Compose(config, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)

	// At minimum: one merge for _defaults, one for _include => >= 2 calls.
	assert.GreaterOrEqual(t, rec.calls(), 2, "configured merger must be invoked for both _defaults and _include")

	// Sanity-check that the configured merger's behavior produced the
	// expected merged output.
	assert.Equal(t, "own_val", result["own"])
	assert.Equal(t, "included_val", result["included_key"])
	assert.Equal(t, "default_val", result["default_key"])
}

// TestComposer_SetMerger verifies the post-construction setter and that
// nil restores the default deep-merge behavior.
func TestComposer_SetMerger(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "inc.yaml"),
		[]byte("k: v\n"),
		0644,
	))

	rec := newRecordingMerger(deepMerger{})
	c := New(dir)
	c.SetMerger(rec)

	_, err := c.Compose(map[string]any{
		"_include": []any{"inc.yaml"},
	}, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, rec.calls(), 1, "SetMerger must install the merger")

	// Reset to default.
	c.SetMerger(nil)
	prev := rec.calls()
	_, err = c.Compose(map[string]any{
		"_include": []any{"inc.yaml"},
	}, filepath.Join(dir, "main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, prev, rec.calls(), "after SetMerger(nil), recordingMerger must no longer be invoked")
}
