// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package selfconfig reads Confii's own configuration from dedicated
// config files before user loaders run.
//
// Search order (first match wins):
//  1. confii.yaml, .yml, .json, .toml in CWD
//  2. .confii.yaml, .yml, .json, .toml in CWD
//  3. Same search in ~/.config/confii/
//
// Settings from the self-config file are applied as defaults: explicit
// constructor arguments always take priority over self-config values.
package selfconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Settings holds Confii self-configuration values.
type Settings struct {
	DefaultEnvironment        string            `yaml:"default_environment" json:"default_environment" toml:"default_environment"`
	EnvSwitcher               string            `yaml:"env_switcher" json:"env_switcher" toml:"env_switcher"`
	DefaultFiles              []string          `yaml:"default_files" json:"default_files" toml:"default_files"`
	DefaultPrefix             string            `yaml:"default_prefix" json:"default_prefix" toml:"default_prefix"`
	EnvPrefix                 string            `yaml:"env_prefix" json:"env_prefix" toml:"env_prefix"`
	SysenvFallback            *bool             `yaml:"sysenv_fallback" json:"sysenv_fallback" toml:"sysenv_fallback"`
	DeepMerge                 *bool             `yaml:"deep_merge" json:"deep_merge" toml:"deep_merge"`
	MergeStrategy             string            `yaml:"merge_strategy" json:"merge_strategy" toml:"merge_strategy"`
	MergeStrategyMap          map[string]string `yaml:"merge_strategy_map" json:"merge_strategy_map" toml:"merge_strategy_map"`
	ValidateOnLoad            *bool             `yaml:"validate_on_load" json:"validate_on_load" toml:"validate_on_load"`
	StrictValidation          *bool             `yaml:"strict_validation" json:"strict_validation" toml:"strict_validation"`
	UseEnvExpander            *bool             `yaml:"use_env_expander" json:"use_env_expander" toml:"use_env_expander"`
	UseTypeCasting            *bool             `yaml:"use_type_casting" json:"use_type_casting" toml:"use_type_casting"`
	DynamicReloading          *bool             `yaml:"dynamic_reloading" json:"dynamic_reloading" toml:"dynamic_reloading"`
	FreezeOnLoad              *bool             `yaml:"freeze_on_load" json:"freeze_on_load" toml:"freeze_on_load"`
	DebugMode                 *bool             `yaml:"debug_mode" json:"debug_mode" toml:"debug_mode"`
	LogLevel                  string            `yaml:"log_level" json:"log_level" toml:"log_level"`
	SchemaPath                string            `yaml:"schema_path" json:"schema_path" toml:"schema_path"`
	EnvironmentStrategy       string            `yaml:"environment_strategy" json:"environment_strategy" toml:"environment_strategy"`
	EnvironmentConflictPolicy string            `yaml:"environment_conflict_policy" json:"environment_conflict_policy" toml:"environment_conflict_policy"`
	// OnError is the error-handling policy applied to loader and
	// composition failures. Valid values are "raise", "warn", and
	// "ignore" (case-insensitive); any other string is rejected by the
	// caller (confii.New) at startup with a typed *confii.ConfigError —
	// invalid values are no longer silently coerced into warn-or-ignore
	// behavior (G07).
	OnError string `yaml:"on_error" json:"on_error" toml:"on_error"`

	// Declarative source definitions (list of {type, path/url, ...} maps).
	Sources []map[string]any `yaml:"sources" json:"sources" toml:"sources"`

	// Declarative secret store configuration ({provider, ...} map).
	Secrets map[string]any `yaml:"secrets" json:"secrets" toml:"secrets"`
}

// searchFiles is the ordered list of self-configuration file candidates.
var searchFiles = []string{
	"confii.yaml", "confii.yml", "confii.json", "confii.toml",
	".confii.yaml", ".confii.yml", ".confii.json", ".confii.toml",
}

// cacheEntry holds the resolved Settings for a specific working directory.
type cacheEntry struct {
	settings *Settings
}

// Module-level cache keyed by absolute working directory path (G06).
//
// Pre-G06 the cache only memoized the literal "." key, which had two
// failure modes: (a) callers using two distinct working directories from
// the same process shared a single cache slot, so the second caller
// observed the first caller's settings; and (b) tests had to clear the
// cache by hand because chdir within a test process would not invalidate
// the "." entry. Keying on filepath.Abs(dir) eliminates both.
var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

// cacheKey resolves dir to its absolute path for use as the cache key.
// On filepath.Abs failure (e.g. the underlying syscall to get the working
// directory failed), the raw dir string is returned as a best-effort key
// so the cache still functions; the contract is "consistent key for the
// same dir argument", not "guaranteed absolute".
func cacheKey(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// Read searches for and reads the self-configuration file.
// It checks the given directory for confii.* files, then falls back
// to ~/.config/confii/.
// Returns nil settings (no error) if no self-config file is found.
//
// Results are cached at module level keyed by the absolute path of dir
// (G06). Two concurrent New calls with different working directories no
// longer share a cache entry, so each gets the self-config that lives
// next to its own dir argument.
func Read(dir string) (*Settings, error) {
	if dir == "" {
		dir = "."
	}

	key := cacheKey(dir)

	cacheMu.Lock()
	if entry, ok := cache[key]; ok {
		result := entry.settings
		cacheMu.Unlock()
		return result, nil
	}
	cacheMu.Unlock()

	settings, err := readFromDir(dir)
	if err != nil {
		return nil, err
	}

	cacheMu.Lock()
	cache[key] = cacheEntry{settings: settings}
	cacheMu.Unlock()

	return settings, nil
}

// ClearCache invalidates the module-level self-config cache for ALL
// keys (G06). Pre-G06 the function only cleared the literal "." entry,
// so a test populating two distinct dirs would only see one purged.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = map[string]cacheEntry{}
}

func readFromDir(dir string) (*Settings, error) {
	// Try dedicated confii files in the given directory.
	for _, name := range searchFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		return readFile(path)
	}

	// Fall back to ~/.config/confii/ (XDG-style).
	if home, err := os.UserHomeDir(); err == nil {
		xdgDir := filepath.Join(home, ".config", "confii")
		for _, name := range searchFiles {
			path := filepath.Join(xdgDir, name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			return readFile(path)
		}
	}

	return nil, nil
}

func readFile(path string) (*Settings, error) {
	// #nosec G304 -- path is assembled internally from fixed self-configuration filenames.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var settings Settings
	ext := filepath.Ext(path)
	switch ext {
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &settings)
	case ".json":
		err = json.Unmarshal(data, &settings)
	case ".toml":
		err = toml.Unmarshal(data, &settings)
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}
