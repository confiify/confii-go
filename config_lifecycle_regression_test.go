// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingJSONFileLoader struct {
	path  string
	calls int
}

func (l *countingJSONFileLoader) Load(context.Context) (map[string]any, error) {
	l.calls++
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (l *countingJSONFileLoader) Source() string { return l.path }

func requireCompletes(t *testing.T, operation func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation deadlocked while invoking a re-entrant event listener")
		return nil
	}
}

func TestLifecycleEventsMayReadConfigWithoutDeadlock(t *testing.T) {
	t.Run("reload_success", func(t *testing.T) {
		l := &scriptedLoader{source: "scripted", script: []scriptStep{
			{data: map[string]any{"key": "before"}},
			{data: map[string]any{"key": "after"}},
		}}
		cfg, err := New[any](context.Background(), WithLoaders(l))
		require.NoError(t, err)
		cfg.EnableEvents().On("reload", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "after", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.Reload(context.Background(), WithIncremental(false))
		})
		require.NoError(t, err)
	})

	t.Run("reload_failure", func(t *testing.T) {
		l := &scriptedLoader{source: "scripted", script: []scriptStep{
			{data: map[string]any{"key": "before"}},
			{err: errors.New("reload failed")},
		}}
		cfg, err := New[any](context.Background(), WithLoaders(l))
		require.NoError(t, err)
		cfg.EnableEvents().On("reload_failed", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "before", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.Reload(context.Background(), WithIncremental(false))
		})
		require.Error(t, err)
	})

	t.Run("extend_success", func(t *testing.T) {
		cfg, err := New[any](context.Background(), WithLoaders(
			&stubLoader{source: "base", data: map[string]any{"key": "before"}},
		))
		require.NoError(t, err)
		cfg.EnableEvents().On("extend", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "after", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.Extend(context.Background(), &stubLoader{
				source: "extension", data: map[string]any{"key": "after"},
			})
		})
		require.NoError(t, err)
	})

	t.Run("extend_failure", func(t *testing.T) {
		cfg, err := New[any](context.Background(), WithLoaders(
			&stubLoader{source: "base", data: map[string]any{"key": "before"}},
		))
		require.NoError(t, err)
		cfg.EnableEvents().On("extend_failed", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "before", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.Extend(context.Background(), &stubLoader{
				source: "broken", err: errors.New("extend failed"),
			})
		})
		require.Error(t, err)
	})
}

func TestGetAutomaticallyRecordsSuccessfulAccesses(t *testing.T) {
	cfg, err := New[any](context.Background(), WithLoaders(
		&stubLoader{source: "base", data: map[string]any{"database": map[string]any{"host": "localhost"}}},
	))
	require.NoError(t, err)
	metrics := cfg.EnableObservability()

	_, err = cfg.Get("database.host")
	require.NoError(t, err)
	_, err = cfg.Get("database.host")
	require.NoError(t, err)
	_, err = cfg.Get("missing")
	require.Error(t, err)

	stats := metrics.Statistics()
	assert.Equal(t, 1, stats["accessed_keys"])
	top, ok := stats["top_accessed_keys"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 2, top["database.host"])
}

func TestIncrementalReloadLoadsOnlyChangedFilesAndReusesOtherLayers(t *testing.T) {
	dir := t.TempDir()
	basePath := dir + "/base.json"
	overridePath := dir + "/override.json"
	require.NoError(t, os.WriteFile(basePath, []byte(`{"base":"v1","shared":"base"}`), 0o600))
	require.NoError(t, os.WriteFile(overridePath, []byte(`{"override":"stable","shared":"override"}`), 0o600))
	base := &countingJSONFileLoader{path: basePath}
	override := &countingJSONFileLoader{path: overridePath}
	cfg, err := New[any](context.Background(), WithLoaders(base, override))
	require.NoError(t, err)

	// Use different content (not just mtime) so the SHA-256 detector is
	// guaranteed to select the base layer even on coarse filesystems.
	require.NoError(t, os.WriteFile(basePath, []byte(`{"base":"v2","shared":"new-base"}`), 0o600))
	err = cfg.Reload(context.Background(), WithIncremental(true))
	require.NoError(t, err)

	assert.Equal(t, 2, base.calls)
	assert.Equal(t, 1, override.calls, "unchanged override file must not be loaded again")
	assert.Equal(t, "v2", cfg.MustGet("base"))
	assert.Equal(t, "stable", cfg.MustGet("override"), "cached unchanged layer must remain present")
	assert.Equal(t, "override", cfg.MustGet("shared"), "cached layer precedence must be preserved")
}

func TestIncrementalReloadRefreshesUntrackableRemoteSources(t *testing.T) {
	remote := &scriptedLoader{source: "https://config.example.test/app.json", script: []scriptStep{
		{data: map[string]any{"version": "v1"}},
		{data: map[string]any{"version": "v2"}},
	}}
	cfg, err := New[any](context.Background(), WithLoaders(remote))
	require.NoError(t, err)
	require.NoError(t, cfg.Reload(context.Background(), WithIncremental(true)))
	assert.Equal(t, "v2", cfg.MustGet("version"))
}

func TestIncrementalReloadDetectsTransitiveCompositionDependency(t *testing.T) {
	dir := t.TempDir()
	topPath := dir + "/top.json"
	includedPath := dir + "/included.json"
	require.NoError(t, os.WriteFile(topPath, []byte(`{"_include":"included.json","top":true}`), 0o600))
	require.NoError(t, os.WriteFile(includedPath, []byte(`{"included":"v1"}`), 0o600))
	l := &countingJSONFileLoader{path: topPath}
	cfg, err := New[any](context.Background(), WithLoaders(l))
	require.NoError(t, err)
	assert.Equal(t, "v1", cfg.MustGet("included"))

	require.NoError(t, os.WriteFile(includedPath, []byte(`{"included":"v2"}`), 0o600))
	require.NoError(t, cfg.Reload(context.Background(), WithIncremental(true)))
	assert.Equal(t, 2, l.calls, "changing an _include dependency must reload its owning layer")
	assert.Equal(t, "v2", cfg.MustGet("included"))
}
