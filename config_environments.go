// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AvailableEnvironments returns the environment names discoverable from the
// Config's resolved sectioned and named-file sources.
//
// For sectioned sources, every top-level mapping alongside the reserved
// "default" mapping is an environment. For environment_files sources, names
// are extracted from files matching the configured {environment} template in
// search-path order. The result is sorted, deduplicated, and does not include
// the reserved shared layer named "default".
//
// Remote or custom loaders cannot always enumerate environments without a
// provider-specific listing operation. Such loaders still participate in
// normal loading, but only environment names observable in their loaded
// sectioned data can be returned here.
func (c *Config[T]) AvailableEnvironments() ([]string, error) {
	return c.availableEnvironments(defaultEnvironmentInventoryFS())
}

type environmentInventoryFS struct {
	abs       func(string) (string, error)
	stat      func(string) (os.FileInfo, error)
	readDir   func(string) ([]os.DirEntry, error)
	entryInfo func(os.DirEntry) (os.FileInfo, error)
}

func defaultEnvironmentInventoryFS() environmentInventoryFS {
	return environmentInventoryFS{
		abs:     filepath.Abs,
		stat:    os.Stat,
		readDir: os.ReadDir,
		entryInfo: func(entry os.DirEntry) (os.FileInfo, error) {
			return entry.Info()
		},
	}
}

func (c *Config[T]) availableEnvironments(fs environmentInventoryFS) ([]string, error) {
	c.mu.RLock()
	layers := copyLoaderLayers(c.loaderLayers)
	loaders := append([]Loader(nil), c.loaders...)
	sources := append([]map[string]any(nil), c.opts.selfConfigSources...)
	root := c.opts.WorkingDir
	env := c.env
	c.mu.RUnlock()

	available := make(map[string]struct{})
	for i, layer := range layers {
		if layer == nil || i >= len(loaders) {
			continue
		}
		if selected, ok := loaders[i].(interface{ selectedEnvironmentFile() bool }); ok && selected.selectedEnvironmentFile() {
			continue
		}
		if !c.envHandler.IsEnvironmentAware(layer, env) {
			continue
		}
		for name, value := range layer {
			if name == "default" {
				continue
			}
			if _, ok := value.(map[string]any); ok {
				available[name] = struct{}{}
			}
		}
	}

	if root == "" {
		root = "."
	}
	absRoot, err := fs.abs(root)
	if err != nil {
		return nil, environmentFilesError("resolve project root for environment inventory", err)
	}
	for _, source := range sources {
		rawType, _ := source["type"].(string)
		typeName := strings.ToLower(strings.TrimSpace(rawType))
		if typeName != "environment_files" && typeName != "environment-files" {
			continue
		}
		cfg, err := parseEnvironmentFilesSource(source)
		if err != nil {
			return nil, err
		}
		if err := discoverNamedEnvironmentsWithFS(absRoot, cfg, available, fs); err != nil {
			return nil, err
		}
	}

	names := make([]string, 0, len(available))
	for name := range available {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func discoverNamedEnvironments(root string, cfg environmentFilesSource, available map[string]struct{}) error {
	return discoverNamedEnvironmentsWithFS(root, cfg, available, defaultEnvironmentInventoryFS())
}

func discoverNamedEnvironmentsWithFS(
	root string,
	cfg environmentFilesSource,
	available map[string]struct{},
	fs environmentInventoryFS,
) error {
	prefix, suffix, _ := strings.Cut(cfg.environmentFile, environmentPlaceholder)
	for _, searchPath := range cfg.searchPaths {
		dir := searchPath
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		info, err := fs.stat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return environmentFilesError("inspect environment search path "+dir, err)
		}
		if !info.IsDir() {
			return environmentFilesError("list environment search path "+dir, errors.New("not a directory"))
		}
		entries, err := fs.readDir(dir)
		if err != nil {
			return environmentFilesError("list environment search path "+dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == cfg.defaultFile {
				continue
			}
			name, ok := environmentNameFromFilename(entry.Name(), prefix, suffix)
			if !ok || name == "default" || validateEnvironmentName(name) != nil {
				continue
			}
			info, err := fs.entryInfo(entry)
			if err != nil {
				return environmentFilesError("inspect environment candidate "+filepath.Join(dir, entry.Name()), err)
			}
			if info.Mode().IsRegular() {
				available[name] = struct{}{}
			}
		}
	}
	return nil
}

func environmentNameFromFilename(filename, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(filename, prefix) || !strings.HasSuffix(filename, suffix) {
		return "", false
	}
	if len(filename) < len(prefix)+len(suffix) {
		return "", false
	}
	name := filename[len(prefix) : len(filename)-len(suffix)]
	return name, name != ""
}
