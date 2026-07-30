// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/confiify/confii-go/v2/envhandler"
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

func TestEnvironmentInventoryFilesystemFailures(t *testing.T) {
	baseFS := defaultEnvironmentInventoryFS()
	newConfig := func() *Config[any] {
		return &Config[any]{
			envHandler: envhandler.New(nil),
			opts: options{selfConfigSources: []map[string]any{{
				"type": "environment_files", "search_paths": []any{"config"},
			}}},
		}
	}

	t.Run("absolute root", func(t *testing.T) {
		injected := baseFS
		injected.abs = func(string) (string, error) { return "", errors.New("abs failed") }
		_, err := newConfig().availableEnvironments(injected)
		require.ErrorContains(t, err, "resolve project root")
	})

	t.Run("stat", func(t *testing.T) {
		injected := baseFS
		injected.stat = func(string) (os.FileInfo, error) { return nil, errors.New("stat failed") }
		_, err := newConfig().availableEnvironments(injected)
		require.ErrorContains(t, err, "inspect environment search path")
	})

	t.Run("read directory", func(t *testing.T) {
		injected := baseFS
		injected.stat = func(string) (os.FileInfo, error) { return inventoryFileInfo{directory: true}, nil }
		injected.readDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("read failed") }
		_, err := newConfig().availableEnvironments(injected)
		require.ErrorContains(t, err, "list environment search path")
	})

	t.Run("entry info", func(t *testing.T) {
		injected := baseFS
		injected.stat = func(string) (os.FileInfo, error) { return inventoryFileInfo{directory: true}, nil }
		injected.readDir = func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{inventoryDirEntry{name: "development.yaml"}}, nil
		}
		injected.entryInfo = func(os.DirEntry) (os.FileInfo, error) { return nil, errors.New("info failed") }
		_, err := newConfig().availableEnvironments(injected)
		require.ErrorContains(t, err, "inspect environment candidate")
	})
}

type inventoryFileInfo struct{ directory bool }

func (i inventoryFileInfo) Name() string { return "fixture" }
func (i inventoryFileInfo) Size() int64  { return 0 }
func (i inventoryFileInfo) Mode() fs.FileMode {
	if i.directory {
		return fs.ModeDir
	}
	return 0
}
func (i inventoryFileInfo) ModTime() time.Time { return time.Time{} }
func (i inventoryFileInfo) IsDir() bool        { return i.directory }
func (i inventoryFileInfo) Sys() any           { return nil }

type inventoryDirEntry struct{ name string }

func (i inventoryDirEntry) Name() string             { return i.name }
func (inventoryDirEntry) IsDir() bool                { return false }
func (inventoryDirEntry) Type() fs.FileMode          { return 0 }
func (inventoryDirEntry) Info() (os.FileInfo, error) { return inventoryFileInfo{}, nil }

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
