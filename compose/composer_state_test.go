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

func TestComposer_VisitedResetBetweenCalls(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.yaml"),
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

	first, err := c.Compose(makeConfig(), filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, "shared_val", first["shared_key"], "first call must include shared file")
	assert.Equal(t, "outer", first["top"])
	nested, ok := first["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deep", nested["inner"])

	second, err := c.Compose(makeConfig(), filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, "shared_val", second["shared_key"], "second call must re-include shared file (visited must reset between calls())")
	assert.Equal(t, "outer", second["top"])
	nested2, ok := second["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deep", nested2["inner"])

	for i := 0; i < 5; i++ {
		out, err := c.Compose(makeConfig(), filepath.Join(dir, "main().yaml"))
		require.NoError(t, err)
		assert.Equal(t, "shared_val", out["shared_key"], "iteration %d must re-include shared file", i)
	}
}

func TestComposer_CycleDetectionIntactWithinSingleCall(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.yaml"),
		[]byte("_include:\n  - b.yaml\nfrom_a: true\n"),
		0644,
	))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.yaml"),
		[]byte("_include:\n  - a.yaml\nfrom_b: true\n"),
		0644,
	))

	config := map[string]any{
		"_include": []any{"a.yaml"},
	}

	c := New(dir)
	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err, "cycle must terminate cleanly")
	assert.Equal(t, true, result["from_a"])
	assert.Equal(t, true, result["from_b"])
	_, hasInclude := result["_include"]
	assert.False(t, hasInclude, "_include directive should be stripped after composition")
}

type recordingMerger struct {
	mu        sync.Mutex
	callCount int

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

func TestComposer_UsesConfiguredMerger(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inc.yaml"),
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

	result, err := c.Compose(config, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)

	assert.GreaterOrEqual(t, rec.calls(), 2, "configured merger must be invoked for both _defaults and _include")

	assert.Equal(t, "own_val", result["own"])
	assert.Equal(t, "included_val", result["included_key"])
	assert.Equal(t, "default_val", result["default_key"])
}

func TestComposer_SetMerger(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "inc.yaml"),
		[]byte("k: v\n"),
		0644,
	))

	rec := newRecordingMerger(deepMerger{})
	c := New(dir)
	c.SetMerger(rec)

	_, err := c.Compose(map[string]any{
		"_include": []any{"inc.yaml"},
	}, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, rec.calls(), 1, "SetMerger must install the merger")

	c.SetMerger(nil)
	prev := rec.calls()
	_, err = c.Compose(map[string]any{
		"_include": []any{"inc.yaml"},
	}, filepath.Join(dir, "main().yaml"))
	require.NoError(t, err)
	assert.Equal(t, prev, rec.calls(), "after SetMerger(nil), recordingMerger must no longer be invoked")
}
