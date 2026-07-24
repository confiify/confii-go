// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

// F-Override-SourceTracking (Wave 16) coverage.
//
// Pre-Wave-16 Override mutated envConfig/mergedConfig but never called
// c.sourceTracker.TrackValue, so Explain reported the pre-Override
// loader source for keys that had been overridden — the same divergence
// Wave 11 G12 cited for Set. Wave 16 closes the parity by:
//
//   1. Snapshotting the tracker before the Override loop so the inverse
//      restore() can roll the tracker back to the pre-Override state
//      (Option A from the audit text — aligns with the Wave 7 G14 / D05
//      Snapshot+Restore pattern Reload uses).
//   2. Calling TrackValue(k, stored, "override", "override", c.env) for
//      each key WHILE holding the write lock and BEFORE the Wave 10 G13
//      lock-release-then-callback step.
//   3. Calling tracker.Restore(trackerSnap) inside restore() so
//      Explain(key).source reverts to the ORIGINAL loader source — not
//      "(restored)" or "override-restored" or "override".
//
// Each test below pins one observable that would fail under the
// pre-Wave-16 behavior.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedOverrideLoader returns a different result for each successive
// Load call. Used to drive Reload during concurrent Override tests and
// to simulate failing reloads for the rollback-contract test.
type scriptedOverrideLoader struct {
	source string
	script []scriptedOverrideStep
	calls  int32
}

type scriptedOverrideStep struct {
	data map[string]any
	err  error
}

func (s *scriptedOverrideLoader) Load(_ context.Context) (map[string]any, error) {
	idx := int(atomic.AddInt32(&s.calls, 1)) - 1
	if idx >= len(s.script) {
		idx = len(s.script) - 1
	}
	st := s.script[idx]
	return st.data, st.err
}

func (s *scriptedOverrideLoader) Source() string { return s.source }

// TestOverride_SourceTrackingParity asserts that a successful Override
// updates the source tracker so Explain reports source="override" for
// the override-mutated key. Pre-Wave-16 Explain reported the loader's
// pre-Override source (e.g. "loader/testdata/simple.yaml") even after
// Override took effect, leaving introspection inconsistent with state.
func TestOverride_SourceTrackingParity(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	// Sanity: pre-Override source is the file loader.
	pre := cfg.Explain("database.host")
	require.Equal(t, true, pre["exists"])
	require.NotEqual(t, "override", pre["source"],
		"fixture precondition: pre-Override source must be the file loader")
	require.NotEqual(t, "runtime", pre["source"],
		"fixture precondition: pre-Override source must be the file loader")
	preSource := pre["source"]

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)

	post := cfg.Explain("database.host")
	require.Equal(t, true, post["exists"])
	assert.Equal(t, "override", post["source"],
		"Explain must report source=override after Override (F-Override-SourceTracking)")
	assert.Equal(t, "override", post["loader_type"],
		"Explain must report loader_type=override after Override (F-Override-SourceTracking)")
	assert.Equal(t, "override-value", post["current_value"])

	// Restore must revert the source to the ORIGINAL loader source —
	// not "(restored)", not "override-restored", not "override".
	restore()
	after := cfg.Explain("database.host")
	require.Equal(t, true, after["exists"])
	assert.Equal(t, preSource, after["source"],
		"restore() must revert Explain.source to the ORIGINAL loader source (Option A snapshot+restore)")
	assert.NotEqual(t, "override", after["source"],
		"restore() must NOT leave the source as \"override\"")
	assert.NotEqual(t, "(restored)", after["source"],
		"restore() must NOT use a \"(restored)\" sentinel")
	assert.NotEqual(t, "override-restored", after["source"],
		"restore() must NOT use an \"override-restored\" sentinel")
}

// TestOverride_OverrideHistoryRecordsOverrideEntry pins that
// GetOverrideHistory(key) shows an "override" entry between the
// original load and the post-Override state. The History slice is only
// populated when DebugMode is enabled (sourcetrack/tracker.go contract),
// so this test passes WithDebugMode(true).
func TestOverride_OverrideHistoryRecordsOverrideEntry(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	preHist := cfg.GetOverrideHistory("database.host")
	require.Empty(t, preHist,
		"fixture precondition: history must be empty before Override")

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)
	defer restore()

	hist := cfg.GetOverrideHistory("database.host")
	require.NotEmpty(t, hist,
		"GetOverrideHistory must contain an entry for the overridden key (F-Override-SourceTracking)")

	// The history entry stores the PREVIOUS (loader) value with the
	// previous source (the YAML loader). The current value+source
	// (override / override-value) lives in SourceInfo proper, not in
	// History — that's the Tracker.TrackValue contract.
	last := hist[len(hist)-1]
	assert.Equal(t, "localhost", last.Value,
		"history must preserve the pre-Override value (was localhost in simple.yaml)")
	assert.NotEqual(t, "override", last.Source,
		"history entry source must be the pre-Override source, not \"override\"")

	// And the current SourceInfo claims the override.
	info := cfg.GetSourceInfo("database.host")
	require.NotNil(t, info)
	assert.Equal(t, "override", info.SourceFile)
	assert.Equal(t, "override", info.LoaderType)
	assert.GreaterOrEqual(t, info.OverrideCount, 1,
		"OverrideCount must increment on Override (F-Override-SourceTracking)")
}

// TestOverride_RestoreTruncatesOverrideHistory pins that after
// restore() runs, the override entry is gone — restore() reverts the
// tracker to its pre-Override snapshot, which had no override entry.
func TestOverride_RestoreTruncatesOverrideHistory(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)

	// Override created the history entry.
	require.NotEmpty(t, cfg.GetOverrideHistory("database.host"))

	restore()

	// After restore() the tracker should be back to the snapshot, which
	// had no history entries (the loader had only just populated the
	// key, no overrides had occurred).
	postHist := cfg.GetOverrideHistory("database.host")
	assert.Empty(t, postHist,
		"restore() must truncate the override history (Option A snapshot+restore)")

	info := cfg.GetSourceInfo("database.host")
	require.NotNil(t, info)
	assert.Equal(t, 0, info.OverrideCount,
		"restore() must reset OverrideCount to the pre-Override value (0)")
	assert.NotEqual(t, "override", info.SourceFile,
		"restore() must revert SourceFile to the pre-Override loader source")
}

// TestOverride_ConcurrentOverrideAndReload runs Override concurrently
// with a Reload that re-reads the same key. Under -race the test must
// pass cleanly, AND a Reload that runs AFTER the Override (with a fresh
// payload for the same key) must overwrite the "override" source claim
// with the loader source — proving tracker correctness across the
// reload cycle.
//
// The deterministic post-condition is checked after both finish: at
// that point, the Reload has re-read the loader, so the source must be
// the loader source (NOT "override").
func TestOverride_ConcurrentOverrideAndReload(t *testing.T) {
	l := &scriptedOverrideLoader{
		source: "scripted-loader",
		script: []scriptedOverrideStep{
			{data: map[string]any{"k": "v1"}},
			{data: map[string]any{"k": "v2"}}, // Reload payload.
			{data: map[string]any{"k": "v2"}},
		},
	}
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(l),
	)
	require.NoError(t, err)

	// Override first so we have a known starting point.
	restore, err := cfg.Override(map[string]any{"k": "override-value"})
	require.NoError(t, err)
	defer restore()

	// Sanity: source is "override" right after Override.
	assert.Equal(t, "override", cfg.Explain("k")["source"])

	// Concurrent Override + Reload: -race must show no data race on
	// the tracker between Override's TrackValue and Reload's Restore +
	// TrackConfig path.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = cfg.Override(map[string]any{"k": "override-2"})
	}()
	go func() {
		defer wg.Done()
		_ = cfg.Reload(context.Background(), confii.WithIncremental(false))
	}()
	wg.Wait()

	// Drive a final Reload to force a deterministic tracker state.
	require.NoError(t, cfg.Reload(context.Background(), confii.WithIncremental(false)))

	post := cfg.Explain("k")
	require.Equal(t, true, post["exists"])
	assert.Equal(t, "scripted-loader", post["source"],
		"a Reload after Override must overwrite the override source claim with the loader source")
}

// TestOverride_ConcurrentOverrideAndSet pins that under -race no race
// is observable between Override's TrackValue and Set's TrackValue.
// After both have completed, restore() must revert to the original
// loader source — proving Set's source claim ("runtime") is preserved
// in the snapshot taken at Override entry, then restored by restore().
func TestOverride_ConcurrentOverrideAndSet(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	preSource := cfg.Explain("database.host")["source"]

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)

	// After Override, run a Set on the SAME key concurrently with another
	// Override on a DIFFERENT key. -race must show no tracker races.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = cfg.Set("database.host", "set-value")
	}()
	go func() {
		defer wg.Done()
		// Override on a different key path so it doesn't fight the Set.
		_, _ = cfg.Override(map[string]any{"database.port": 6543})
	}()
	wg.Wait()

	// restore() must roll back to the original LOADER source (not to
	// "runtime", because Set ran AFTER Override entered — but the
	// snapshot was taken BEFORE Override, capturing the loader source).
	restore()

	after := cfg.Explain("database.host")
	require.Equal(t, true, after["exists"])
	assert.Equal(t, preSource, after["source"],
		"restore() must revert to the pre-Override (loader) source even after a concurrent Set")
}

// TestOverride_FailingReloadRollbackPreservesContract pins that
// Wave 7 G14's Reload-rollback contract still holds when Override is
// in the picture. The flow:
//
//  1. Override sets source=override.
//  2. Reload fails. Reload's rollback restores the tracker to the
//     snapshot it took at Reload entry — which is the post-Override
//     tracker (source=override).
//  3. Therefore after the failing Reload, source must STILL be
//     "override" (the post-Override state pre-Reload).
//  4. restore() then reverts source to the original loader source.
//
// This proves: (a) Wave 7 G14's tracker rollback inside Reload is
// independent of Override's snapshot/restore, (b) Override's tracker
// state is correctly carried across a failing Reload.
func TestOverride_FailingReloadRollbackPreservesContract(t *testing.T) {
	l := &scriptedOverrideLoader{
		source: "scripted-loader",
		script: []scriptedOverrideStep{
			{data: map[string]any{"k": "v1"}},
			{err: errors.New("simulated reload failure")},
		},
	}
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(l),
		confii.WithOnError(confii.ErrorPolicyRaise),
	)
	require.NoError(t, err)

	// Pre-Override: loader source.
	pre := cfg.Explain("k")
	require.Equal(t, "scripted-loader", pre["source"])

	restore, err := cfg.Override(map[string]any{"k": "override-value"})
	require.NoError(t, err)
	require.NotNil(t, restore)

	// Source should now be "override".
	mid := cfg.Explain("k")
	require.Equal(t, "override", mid["source"])
	require.Equal(t, "override-value", mid["current_value"])

	// Failing Reload: Reload's rollback (Wave 7 G14) restores the
	// tracker to the post-Override state. After the failing Reload the
	// envConfig/mergedConfig and tracker must all be exactly what they
	// were BEFORE the Reload — i.e. the post-Override state.
	err = cfg.Reload(context.Background(), confii.WithIncremental(false))
	require.Error(t, err, "Reload must propagate the loader failure")

	postReload := cfg.Explain("k")
	assert.Equal(t, true, postReload["exists"])
	assert.Equal(t, "override", postReload["source"],
		"Wave 7 G14 rollback must preserve the post-Override tracker state across a failing Reload")
	assert.Equal(t, "override-value", postReload["current_value"],
		"Wave 7 G14 rollback must preserve the post-Override envConfig across a failing Reload")

	// restore() then unwinds back to the pre-Override loader source.
	restore()

	postRestore := cfg.Explain("k")
	assert.Equal(t, "scripted-loader", postRestore["source"],
		"restore() after a failing Reload must still revert to the pre-Override loader source")
}
