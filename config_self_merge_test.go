// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelfConfigMergeStrategyAndMap(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
merge_strategy: merge
merge_strategy_map:
  servers: append
`), 0o600))
	base := filepath.Join(dir, "base.yaml")
	overlay := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(base, []byte("servers: [one]\nbase_only: true\n"), 0o600))
	require.NoError(t, os.WriteFile(overlay, []byte("servers: [two]\noverlay_only: true\n"), 0o600))

	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)
	cfg, err := confii.New[any](context.Background(),
		confii.WithWorkingDir(dir),
		confii.WithLoaders(loader.NewYAML(base), loader.NewYAML(overlay)),
	)
	require.NoError(t, err)

	servers, err := cfg.Get("servers")
	require.NoError(t, err)
	assert.Equal(t, []any{"one", "two"}, servers)
	assert.True(t, cfg.Has("base_only"))
	assert.True(t, cfg.Has("overlay_only"))
}

func TestGeneratedSelfConfigIsUsableUnchanged(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), selfconfig.DefaultYAML(), 0o600))
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(dir))
	require.NoError(t, err)
	assert.Empty(t, cfg.ToDict())
	assert.Equal(t, confii.EnvironmentStrategyAuto, cfg.SourcePlan().Strategy)
}

func TestExplicitMergeStrategyWinsOverSelfConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte("merge_strategy: replace\n"), 0o600))
	base := filepath.Join(dir, "base.yaml")
	overlay := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(base, []byte("base_only: true\n"), 0o600))
	require.NoError(t, os.WriteFile(overlay, []byte("overlay_only: true\n"), 0o600))

	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)
	cfg, err := confii.New[any](context.Background(),
		confii.WithWorkingDir(dir),
		confii.WithLoaders(loader.NewYAML(base), loader.NewYAML(overlay)),
		confii.WithMergeStrategyOption(confii.StrategyMerge),
	)
	require.NoError(t, err)
	assert.True(t, cfg.Has("base_only"))
	assert.True(t, cfg.Has("overlay_only"))
}

func TestExplicitMergeStrategyMapWinsOverSelfConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte(`
merge_strategy_map:
  servers: append
`), 0o600))
	base := filepath.Join(dir, "base.yaml")
	overlay := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(base, []byte("servers: [one]\n"), 0o600))
	require.NoError(t, os.WriteFile(overlay, []byte("servers: [two]\n"), 0o600))

	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)
	cfg, err := confii.New[any](context.Background(),
		confii.WithWorkingDir(dir),
		confii.WithLoaders(loader.NewYAML(base), loader.NewYAML(overlay)),
		confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
			"servers": confii.StrategyReplace,
		}),
	)
	require.NoError(t, err)

	servers, err := cfg.Get("servers")
	require.NoError(t, err)
	assert.Equal(t, []any{"two"}, servers)
}

func TestSelfConfigMergeStrategyValidation(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte("merge_strategy: concatenate\n"), 0o600))
		selfconfig.ClearCache()
		t.Cleanup(selfconfig.ClearCache)

		_, err := confii.New[any](context.Background(), confii.WithWorkingDir(dir))
		require.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigLoad))
		assert.Contains(t, err.Error(), "concatenate")
	})

	t.Run("path override", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte("merge_strategy_map:\n  servers: concatenate\n"), 0o600))
		selfconfig.ClearCache()
		t.Cleanup(selfconfig.ClearCache)

		_, err := confii.New[any](context.Background(), confii.WithWorkingDir(dir))
		require.Error(t, err)
		assert.True(t, errors.Is(err, confii.ErrConfigLoad))
		assert.Contains(t, err.Error(), "servers")
	})
}
