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
	DefaultEnvironment string   `yaml:"default_environment" json:"default_environment" toml:"default_environment"`
	EnvSwitcher        string   `yaml:"env_switcher" json:"env_switcher" toml:"env_switcher"`
	DefaultFiles       []string `yaml:"default_files" json:"default_files" toml:"default_files"`
	DefaultPrefix      string   `yaml:"default_prefix" json:"default_prefix" toml:"default_prefix"`
	EnvPrefix          string   `yaml:"env_prefix" json:"env_prefix" toml:"env_prefix"`
	SysenvFallback     *bool    `yaml:"sysenv_fallback" json:"sysenv_fallback" toml:"sysenv_fallback"`
	DeepMerge          *bool    `yaml:"deep_merge" json:"deep_merge" toml:"deep_merge"`
	ValidateOnLoad     *bool    `yaml:"validate_on_load" json:"validate_on_load" toml:"validate_on_load"`
	StrictValidation   *bool    `yaml:"strict_validation" json:"strict_validation" toml:"strict_validation"`
	UseEnvExpander     *bool    `yaml:"use_env_expander" json:"use_env_expander" toml:"use_env_expander"`
	UseTypeCasting     *bool    `yaml:"use_type_casting" json:"use_type_casting" toml:"use_type_casting"`
	DynamicReloading   *bool    `yaml:"dynamic_reloading" json:"dynamic_reloading" toml:"dynamic_reloading"`
	FreezeOnLoad       *bool    `yaml:"freeze_on_load" json:"freeze_on_load" toml:"freeze_on_load"`
	DebugMode          *bool    `yaml:"debug_mode" json:"debug_mode" toml:"debug_mode"`
	LogLevel           string   `yaml:"log_level" json:"log_level" toml:"log_level"`
	SchemaPath         string   `yaml:"schema_path" json:"schema_path" toml:"schema_path"`
	OnError            string   `yaml:"on_error" json:"on_error" toml:"on_error"`

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

// Module-level cache for CWD lookups.
var (
	cacheMu      sync.Mutex
	cachedDir    string
	cachedResult *Settings
	cacheLoaded  bool
)

// Read searches for and reads the self-configuration file.
// It checks the given directory for confii.* files, then falls back
// to ~/.config/confii/.
// Returns nil settings (no error) if no self-config file is found.
// Results are cached at module level when dir is "." or "".
func Read(dir string) (*Settings, error) {
	if dir == "" {
		dir = "."
	}

	// Check cache for CWD lookups.
	if dir == "." {
		cacheMu.Lock()
		if cacheLoaded && cachedDir == dir {
			result := cachedResult
			cacheMu.Unlock()
			return result, nil
		}
		cacheMu.Unlock()
	}

	settings, err := readFromDir(dir)
	if err != nil {
		return nil, err
	}

	// Cache CWD lookups.
	if dir == "." {
		cacheMu.Lock()
		cachedDir = dir
		cachedResult = settings
		cacheLoaded = true
		cacheMu.Unlock()
	}

	return settings, nil
}

// ClearCache invalidates the module-level self-config cache.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheLoaded = false
	cachedResult = nil
	cachedDir = ""
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
