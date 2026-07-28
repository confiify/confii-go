// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/confiify/confii-go/envhandler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverNamedEnvironmentsEdgeCases(t *testing.T) {
	root := t.TempDir()
	available := map[string]struct{}{}
	cfg := environmentFilesSource{
		searchPaths:     []string{"missing"},
		defaultFile:     "default.yaml",
		environmentFile: "{environment}.yaml",
	}
	require.NoError(t, discoverNamedEnvironments(root, cfg, available))
	assert.Empty(t, available)

	notDirectory := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(notDirectory, []byte("x"), 0o600))
	cfg.searchPaths = []string{notDirectory}
	require.ErrorContains(t, discoverNamedEnvironments(root, cfg, available), "list environment search path")
}

func TestEnvironmentNameFromFilename(t *testing.T) {
	name, ok := environmentNameFromFilename("app-production.yaml", "app-", ".yaml")
	assert.True(t, ok)
	assert.Equal(t, "production", name)

	_, ok = environmentNameFromFilename("other-production.yaml", "app-", ".yaml")
	assert.False(t, ok)
	_, ok = environmentNameFromFilename("abc", "ab", "bc")
	assert.False(t, ok)
	_, ok = environmentNameFromFilename("app-.yaml", "app-", ".yaml")
	assert.False(t, ok)
}

func TestAvailableEnvironmentsInternalBranches(t *testing.T) {
	t.Run("flat and non-environment sources are ignored", func(t *testing.T) {
		cfg := &Config[any]{
			loaderLayers: []map[string]any{nil, {"app": map[string]any{"name": "demo"}}},
			loaders:      []Loader{stubEnvironmentInventoryLoader{}, stubEnvironmentInventoryLoader{}},
			envHandler:   envhandler.New(nil),
			opts: options{selfConfigSources: []map[string]any{
				{"type": "yaml", "path": "config.yaml"},
			}},
		}
		environments, err := cfg.AvailableEnvironments()
		require.NoError(t, err)
		assert.Empty(t, environments)
	})

	t.Run("invalid environment source", func(t *testing.T) {
		cfg := &Config[any]{
			envHandler: envhandler.New(nil),
			opts: options{selfConfigSources: []map[string]any{
				{"type": "environment_files", "environment_file": "fixed.yaml"},
			}},
		}
		_, err := cfg.AvailableEnvironments()
		require.ErrorContains(t, err, "must contain the {environment} placeholder")
	})

	t.Run("unreadable search path", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "not-a-directory")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
		cfg := &Config[any]{
			envHandler: envhandler.New(nil),
			opts: options{WorkingDir: root, selfConfigSources: []map[string]any{
				{"type": "environment_files", "search_paths": []any{"not-a-directory"}},
			}},
		}
		_, err := cfg.AvailableEnvironments()
		require.ErrorContains(t, err, "list environment search path")
	})
}

type stubEnvironmentInventoryLoader struct{}

func (stubEnvironmentInventoryLoader) Source() string { return "stub" }

func (stubEnvironmentInventoryLoader) Load(context.Context) (map[string]any, error) {
	return nil, nil
}
