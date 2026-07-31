// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package selfconfig reads Confii's own configuration from dedicated
// config files before user loaders run.
//
// Discovery selects exactly one naming family and one format. Hidden project
// files (`.confii.<ext>`) are preferred when they are the only family present;
// visible files (`confii.<ext>`) are the fallback. Multiple formats or a mix of
// hidden and visible families are rejected as ambiguous.
//
// After the base file is decoded, its env_switcher (falling back to
// default_environment) selects an optional matching environment overlay:
// `.confii.<environment>.<ext>` or `confii.<environment>.<ext>`. The overlay
// recursively overrides the base before strict Settings decoding.
//
// Settings from the self-config file are applied as defaults: explicit
// constructor arguments always take priority over self-config values.
package selfconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/confiify/confii-go/v2/internal/formatparse"
	"go.yaml.in/yaml/v3"
)

// Settings is the strictly decoded project-level Confii configuration. Pointer
// booleans distinguish an omitted setting from an explicit false value, which
// allows constructor options to override only settings the project declared.
type Settings struct {
	// DefaultEnvironment is used when neither an explicit constructor option
	// nor EnvSwitcher selects an environment.
	DefaultEnvironment string `yaml:"default_environment" json:"default_environment" toml:"default_environment"`
	// EnvSwitcher names the operating-system variable that selects the active
	// environment and its optional self-config overlay.
	EnvSwitcher string `yaml:"env_switcher" json:"env_switcher" toml:"env_switcher"`
	// EnvPrefix is removed from process-environment keys before nested key
	// conversion. An empty value means no prefix filter.
	EnvPrefix string `yaml:"env_prefix" json:"env_prefix" toml:"env_prefix"`
	// SysenvFallback enables lookup from process environment when a key is not
	// present in loaded configuration.
	SysenvFallback *bool `yaml:"sysenv_fallback" json:"sysenv_fallback" toml:"sysenv_fallback"`
	// Merge defines the default and per-path merge strategies.
	Merge MergeSettings `yaml:"merge" json:"merge" toml:"merge"`
	// ValidateOnLoad requests validation before each candidate snapshot is published.
	ValidateOnLoad *bool `yaml:"validate_on_load" json:"validate_on_load" toml:"validate_on_load"`
	// StrictValidation rejects unknown typed-configuration fields when enabled.
	StrictValidation *bool `yaml:"strict_validation" json:"strict_validation" toml:"strict_validation"`
	// UseEnvExpander enables expansion of environment-variable expressions.
	UseEnvExpander *bool `yaml:"use_env_expander" json:"use_env_expander" toml:"use_env_expander"`
	// UseTypeCasting converts supported scalar strings to bool, int, or float values.
	UseTypeCasting *bool `yaml:"use_type_casting" json:"use_type_casting" toml:"use_type_casting"`
	// DynamicReloading watches local file sources and republishes valid changes.
	DynamicReloading *bool `yaml:"dynamic_reloading" json:"dynamic_reloading" toml:"dynamic_reloading"`
	// FreezeOnLoad freezes the Config after successful initialization.
	FreezeOnLoad *bool `yaml:"freeze_on_load" json:"freeze_on_load" toml:"freeze_on_load"`
	// DebugMode retains additional source values and diagnostic metadata.
	DebugMode *bool `yaml:"debug_mode" json:"debug_mode" toml:"debug_mode"`
	// LogLevel selects the minimum Confii log level.
	LogLevel string `yaml:"log_level" json:"log_level" toml:"log_level"`
	// SchemaPath identifies a JSON Schema file resolved from the project working directory.
	SchemaPath string `yaml:"schema_path" json:"schema_path" toml:"schema_path"`
	// EnvironmentStrategy selects auto, sectioned, named_files, or hybrid interpretation.
	EnvironmentStrategy string `yaml:"environment_strategy" json:"environment_strategy" toml:"environment_strategy"`
	// EnvironmentConflictPolicy selects error, section-wins, or flat-wins behavior.
	EnvironmentConflictPolicy string `yaml:"environment_conflict_policy" json:"environment_conflict_policy" toml:"environment_conflict_policy"`
	// Startup configures construction-time context behavior.
	Startup StartupSettings `yaml:"startup" json:"startup" toml:"startup"`
	// Runtime configures implicit contexts used by context-free operations.
	Runtime RuntimeSettings `yaml:"runtime" json:"runtime" toml:"runtime"`
	// SecretResolutionConcurrency bounds parallel secret resolution; values
	// below one are rejected during Config construction.
	SecretResolutionConcurrency *int `yaml:"secret_resolution_concurrency" json:"secret_resolution_concurrency" toml:"secret_resolution_concurrency"`
	// OnError is the error-handling policy applied to loader and
	// composition failures. Valid values are "raise", "warn", and
	// "ignore" (case-insensitive); any other string is rejected by the
	// caller (confii.New) at startup with a typed *confii.ConfigError —
	// invalid values are never silently coerced into warn-or-ignore behavior.
	OnError string `yaml:"on_error" json:"on_error" toml:"on_error"`

	// Declarative source definitions (list of {type, path/url, ...} maps).
	Sources []map[string]any `yaml:"sources" json:"sources" toml:"sources"`

	// Declarative secret-store configuration containing providers and defaults.
	Secrets map[string]any `yaml:"secrets" json:"secrets" toml:"secrets"`
}

// StartupSettings controls the bounded initialization lifecycle.
type StartupSettings struct {
	// Timeout uses Go duration syntax, such as "30s" or "2m". "0s" disables
	// Confii's fallback deadline but does not remove a caller context deadline.
	Timeout string `yaml:"timeout" json:"timeout" toml:"timeout"`
}

// RuntimeSettings controls implicit contexts used by convenience APIs and
// watcher-triggered reloads.
type RuntimeSettings struct {
	// Timeout uses Go duration syntax and bounds context-free runtime methods.
	// "0s" disables Confii's operation deadline.
	Timeout string `yaml:"timeout" json:"timeout" toml:"timeout"`
}

// MergeSettings defines one canonical merge policy for source composition.
type MergeSettings struct {
	// Default is the strategy applied when no path-specific strategy matches.
	Default string `yaml:"default" json:"default" toml:"default"`
	// Paths maps dot-separated key paths to merge strategy names. The most
	// specific matching path takes precedence.
	Paths map[string]string `yaml:"paths" json:"paths" toml:"paths"`
}

var (
	selfConfigExtensions = []string{"yaml", "yml", "json", "toml"}
	selfConfigFamilies   = []string{".confii", "confii"}
	selfConfigEnvPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// CandidateFilenames returns the self-configuration filenames in discovery
// order. The returned slice is an independent copy so project-management
// tools such as `confii init` can inspect local initialization state without
// mutating the reader's authoritative search order.
func CandidateFilenames() []string {
	result := make([]string, 0, len(selfConfigFamilies)*len(selfConfigExtensions))
	for _, family := range selfConfigFamilies {
		for _, extension := range selfConfigExtensions {
			result = append(result, family+"."+extension)
		}
	}
	return result
}

// cacheEntry holds the resolved Settings for a specific working directory.
type cacheEntry struct {
	settings      *Settings
	envSwitcher   string
	switcherValue string
}

// Module-level cache keyed by absolute working directory path. Distinct working
// directories never share self-config state.
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

// Read discovers and decodes self-configuration in dir, then falls back to
// ~/.config/confii when the project has no self-config file.
// Returns nil settings (no error) if no self-config file is found.
// A discovered file is decoded strictly: unknown top-level fields, malformed
// input, and trailing YAML/JSON documents return an error. Provider-specific
// keys nested inside Sources and Secrets remain extensible.
//
// Results are cached by absolute working directory and invalidated when the
// selected environment-switcher value changes. The returned Settings is
// cache-owned and must be treated as read-only. Call [ClearCache] after changing
// a self-config file in a long-running process.
func Read(dir string) (*Settings, error) {
	if dir == "" {
		dir = "."
	}

	key := cacheKey(dir)

	cacheMu.Lock()
	if entry, ok := cache[key]; ok {
		if entry.envSwitcher == "" || os.Getenv(entry.envSwitcher) == entry.switcherValue {
			result := entry.settings
			cacheMu.Unlock()
			return result, nil
		}
		delete(cache, key)
	}
	cacheMu.Unlock()

	settings, err := readFromDir(dir)
	if err != nil {
		return nil, err
	}

	cacheMu.Lock()
	entry := cacheEntry{settings: settings}
	if settings != nil {
		entry.envSwitcher = settings.EnvSwitcher
		if entry.envSwitcher != "" {
			entry.switcherValue = os.Getenv(entry.envSwitcher)
		}
	}
	cache[key] = entry
	cacheMu.Unlock()

	return settings, nil
}

// ClearCache invalidates all cached self-configuration. It is safe for
// concurrent use and affects only subsequent Read calls.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = map[string]cacheEntry{}
}

func readFromDir(dir string) (*Settings, error) {
	// Try dedicated confii files in the given directory.
	settings, found, err := readFirstFromDir(dir)
	if err != nil || found {
		return settings, err
	}

	// Fall back to ~/.config/confii/ (XDG-style).
	if home, err := os.UserHomeDir(); err == nil {
		settings, _, readErr := readFirstFromDir(filepath.Join(home, ".config", "confii"))
		return settings, readErr
	}

	return nil, nil
}

func readFirstFromDir(dir string) (*Settings, bool, error) {
	base, found, err := discoverBaseFile(dir)
	if err != nil || !found {
		return nil, found, err
	}
	baseMap, err := readFileMap(base.path)
	if err != nil {
		return nil, true, err
	}
	baseSettings, err := decodeSettingsMap(baseMap, base.extension, base.path)
	if err != nil {
		return nil, true, err
	}
	environment := baseSettings.DefaultEnvironment
	if baseSettings.EnvSwitcher != "" {
		if selected := strings.TrimSpace(os.Getenv(baseSettings.EnvSwitcher)); selected != "" {
			environment = selected
		}
	}
	if environment == "" {
		return baseSettings, true, nil
	}
	if !selfConfigEnvPattern.MatchString(environment) || environment == "." || environment == ".." || strings.Contains(environment, "..") {
		return nil, true, fmt.Errorf("invalid self-config environment %q selected by %s", environment, baseSettings.EnvSwitcher)
	}
	overlay, overlayFound, err := discoverEnvironmentFile(dir, base, environment)
	if err != nil {
		return nil, true, err
	}
	if !overlayFound {
		return baseSettings, true, nil
	}
	overlayMap, err := readFileMap(overlay.path)
	if err != nil {
		return nil, true, err
	}
	merged := mergeSettingsMaps(baseMap, overlayMap)
	settings, err := decodeSettingsMap(merged, base.extension, base.path+" + "+overlay.path)
	return settings, true, err
}

type discoveredSelfConfig struct {
	path      string
	family    string
	extension string
}

func discoverBaseFile(dir string) (discoveredSelfConfig, bool, error) {
	found := make(map[string][]discoveredSelfConfig, len(selfConfigFamilies))
	for _, family := range selfConfigFamilies {
		for _, extension := range selfConfigExtensions {
			candidate := discoveredSelfConfig{
				path: filepath.Join(dir, family+"."+extension), family: family, extension: extension,
			}
			exists, err := regularSelfConfigFile(candidate.path)
			if err != nil {
				return discoveredSelfConfig{}, false, err
			}
			if exists {
				found[family] = append(found[family], candidate)
			}
		}
	}
	if len(found[".confii"]) > 0 && len(found["confii"]) > 0 {
		return discoveredSelfConfig{}, false, selfConfigAmbiguityError("hidden and visible self-config files cannot be mixed", found)
	}
	for _, family := range selfConfigFamilies {
		matches := found[family]
		if len(matches) > 1 {
			return discoveredSelfConfig{}, false, selfConfigAmbiguityError("multiple self-config formats are not supported", found)
		}
		if len(matches) == 1 {
			return matches[0], true, nil
		}
	}
	return discoveredSelfConfig{}, false, nil
}

func discoverEnvironmentFile(dir string, base discoveredSelfConfig, environment string) (discoveredSelfConfig, bool, error) {
	found := make(map[string][]discoveredSelfConfig, len(selfConfigFamilies))
	for _, family := range selfConfigFamilies {
		for _, extension := range selfConfigExtensions {
			candidate := discoveredSelfConfig{
				path: filepath.Join(dir, family+"."+environment+"."+extension), family: family, extension: extension,
			}
			exists, err := regularSelfConfigFile(candidate.path)
			if err != nil {
				return discoveredSelfConfig{}, false, err
			}
			if exists {
				found[family] = append(found[family], candidate)
			}
		}
	}
	all := append(append([]discoveredSelfConfig(nil), found[".confii"]...), found["confii"]...)
	if len(all) == 0 {
		return discoveredSelfConfig{}, false, nil
	}
	if len(all) > 1 || all[0].family != base.family || all[0].extension != base.extension {
		return discoveredSelfConfig{}, false, selfConfigAmbiguityError(
			fmt.Sprintf("environment self-config for %q must use the base file's %s.%s convention", environment, base.family, base.extension), found,
		)
	}
	return all[0], true, nil
}

func regularSelfConfigFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect self-config %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("self-config %s is not a regular file", path)
	}
	return true, nil
}

func selfConfigAmbiguityError(reason string, found map[string][]discoveredSelfConfig) error {
	paths := make([]string, 0)
	for _, matches := range found {
		for _, match := range matches {
			paths = append(paths, match.path)
		}
	}
	sort.Strings(paths)
	return fmt.Errorf("ambiguous self-configuration: %s (%s)", reason, strings.Join(paths, ", "))
}

func mergeSettingsMaps(base, overlay map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(overlay))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range overlay {
		baseMap, baseOK := result[key].(map[string]any)
		overlayMap, overlayOK := value.(map[string]any)
		if baseOK && overlayOK {
			result[key] = mergeSettingsMaps(baseMap, overlayMap)
			continue
		}
		result[key] = value
	}
	return result
}

func readFile(path string) (*Settings, error) {
	values, err := readFileMap(path)
	if err != nil {
		return nil, err
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return decodeSettingsMap(values, extension, path)
}

func readFileMap(path string) (map[string]any, error) {
	// #nosec G304 -- path is assembled internally from constrained self-configuration names.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := make(map[string]any)
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	switch extension {
	case "yaml", "yml":
		if err = formatparse.ValidateDeclaredContent(formatparse.FormatYAML, data); err != nil {
			break
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		if err = decoder.Decode(&values); err != nil {
			if err != io.EOF {
				break
			}
			err = nil
		}
		var trailing any
		if trailingErr := decoder.Decode(&trailing); trailingErr != io.EOF {
			if trailingErr == nil {
				err = trailingDocumentError("YAML")
			} else {
				err = trailingErr
			}
		}
	case "json":
		decoder := json.NewDecoder(bytes.NewReader(data))
		if err = decoder.Decode(&values); err != nil {
			// An empty document selects defaults, matching the YAML and
			// TOML branches.
			if err != io.EOF {
				break
			}
			err = nil
		}
		var trailing any
		if trailingErr := decoder.Decode(&trailing); trailingErr != io.EOF {
			if trailingErr == nil {
				err = trailingDocumentError("JSON")
			} else {
				err = trailingErr
			}
		}
	case "toml":
		if err = formatparse.ValidateDeclaredContent(formatparse.FormatTOML, data); err == nil {
			_, err = toml.Decode(string(data), &values)
		}
	default:
		err = fmt.Errorf("unsupported self-config extension %q", filepath.Ext(path))
	}
	if err != nil {
		return nil, fmt.Errorf("parse self-config %s: %w", path, err)
	}
	if values == nil {
		values = make(map[string]any)
	}
	return values, nil
}

func decodeSettingsMap(values map[string]any, extension, source string) (*Settings, error) {
	var (
		data []byte
		err  error
	)
	switch extension {
	case "yaml", "yml":
		data, err = yaml.Marshal(values)
	case "json":
		data, err = json.Marshal(values)
	case "toml":
		var buffer bytes.Buffer
		err = toml.NewEncoder(&buffer).Encode(values)
		data = buffer.Bytes()
	default:
		err = fmt.Errorf("unsupported self-config extension %q", extension)
	}
	if err != nil {
		return nil, fmt.Errorf("normalize self-config %s: %w", source, err)
	}

	var settings Settings
	switch extension {
	case "yaml", "yml":
		err = decodeYAML(data, &settings)
	case "json":
		err = decodeJSON(data, &settings)
	case "toml":
		err = decodeTOML(data, &settings)
	}
	if err != nil {
		return nil, fmt.Errorf("parse self-config %s: %w", source, err)
	}
	return &settings, nil
}

// Self-configuration controls Confii's loading and validation behavior, so a
// misspelled top-level key must never be silently ignored. The nested Sources
// and Secrets maps intentionally remain open-ended for provider-specific
// fields; strictness applies to the Settings schema itself.
func decodeYAML(data []byte, settings *Settings) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(settings); err != nil && err != io.EOF {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return trailingDocumentError("YAML")
		}
		return err
	}
	return nil
}

func decodeJSON(data []byte, settings *Settings) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(settings); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return trailingDocumentError("JSON")
		}
		return err
	}
	return nil
}

func decodeTOML(data []byte, settings *Settings) error {
	metadata, err := toml.Decode(string(data), settings)
	if err != nil {
		return err
	}
	undecoded := metadata.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}
	keys := make([]string, 0, len(undecoded))
	for _, key := range undecoded {
		keys = append(keys, key.String())
	}
	return fmt.Errorf("unknown self-config field(s): %s", strings.Join(keys, ", "))
}

func trailingDocumentError(format string) error {
	return fmt.Errorf("self-config must contain exactly one %s document", format)
}
