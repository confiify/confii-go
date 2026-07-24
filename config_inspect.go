package confii

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/sourcetrack"
	"gopkg.in/yaml.v3"
)

// Explain returns detailed resolution information for a key.
//
// D08 (Wave 13): every value embedded in the returned map is defensively
// deep-copied via [dictutil.DeepCopyValue] so a caller mutating the
// result cannot bleed into envConfig or into the tracker's *SourceInfo.
// info.History is already a fresh slice (D06 GetSourceInfo defensive
// copy), but the per-entry Value field is shallow-copied at the
// SourceInfo layer; this method copies it again on the way out so the
// override_history payload is fully isolated.
func (c *Config[T]) Explain(keyPath string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info := c.sourceTracker.GetSourceInfo(keyPath)
	if info == nil {
		return map[string]any{
			"exists":         false,
			"key":            keyPath,
			"available_keys": dictutil.FlatKeys(c.envConfig),
		}
	}

	result := map[string]any{
		"exists":         true,
		"key":            keyPath,
		"value":          dictutil.DeepCopyValue(info.Value),
		"source":         info.SourceFile,
		"loader_type":    info.LoaderType,
		"environment":    c.env,
		"override_count": info.OverrideCount,
	}

	if len(info.History) > 0 {
		history := make([]map[string]any, 0, len(info.History))
		for _, h := range info.History {
			history = append(history, map[string]any{
				"value":       dictutil.DeepCopyValue(h.Value),
				"source":      h.Source,
				"loader_type": h.LoaderType,
			})
		}
		result["override_history"] = history
	}

	// Current value from live config — deep-copied so a caller mutating
	// the returned map cannot leak into envConfig (D08 / G10 parity).
	if val, ok := dictutil.GetNested(c.envConfig, keyPath); ok {
		result["current_value"] = dictutil.DeepCopyValue(val)
	}

	return result
}

// Schema returns schema information for a key.
//
// D08 (Wave 13): the embedded "value" is deep-copied via
// [dictutil.DeepCopyValue] so a caller mutating the returned map cannot
// alias envConfig (G10 parity on the introspection axis).
func (c *Config[T]) Schema(keyPath string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := map[string]any{"key": keyPath}

	val, ok := dictutil.GetNested(c.envConfig, keyPath)
	if !ok {
		result["exists"] = false
		return result
	}

	result["exists"] = true
	result["value"] = dictutil.DeepCopyValue(val)
	result["type"] = fmt.Sprintf("%T", val)

	return result
}

// Layers returns the layer stack showing each source and its keys.
//
// D08 (Wave 13): the per-layer map and its "keys" slice are freshly
// allocated on every call. Keys come from [sourcetrack.Tracker.FindKeysFromSource]
// which already returns a fresh []string. Mutations on any layer map
// returned here cannot bleed into tracker or envConfig state.
func (c *Config[T]) Layers() []map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := make(map[string]bool)
	var layers []map[string]any

	for _, l := range c.loaders {
		source := l.Source()
		if seen[source] {
			continue
		}
		seen[source] = true

		loaderType := loaderTypeName(l)

		keys := c.sourceTracker.FindKeysFromSource(source)
		// Defensive copy of the layer map: each successive call returns
		// a freshly-allocated map literal so the audit-flagged aliasing
		// hazard cannot reproduce. The "keys" slice is already a fresh
		// allocation from FindKeysFromSource; we copy the header into a
		// new []string so a caller appending or rewriting it cannot
		// reach across calls.
		keysCopy := append([]string(nil), keys...)
		layers = append(layers, map[string]any{
			"source":      source,
			"loader_type": loaderType,
			"keys":        keysCopy,
			"key_count":   len(keysCopy),
		})
	}
	return layers
}

// SourcePlan returns the environment strategy, ordered source roles, and any
// mixed-model conflicts observed during the last successful load. The result
// is defensively copied and safe for callers to mutate.
func (c *Config[T]) SourcePlan() SourcePlan {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneSourcePlan(c.sourcePlan)
}

// GetSourceInfo returns source tracking info for a key.
func (c *Config[T]) GetSourceInfo(keyPath string) *sourcetrack.SourceInfo {
	return c.sourceTracker.GetSourceInfo(keyPath)
}

// GetOverrideHistory returns the override history for a key.
func (c *Config[T]) GetOverrideHistory(keyPath string) []sourcetrack.OverrideEntry {
	return c.sourceTracker.GetOverrideHistory(keyPath)
}

// GetConflicts returns all keys that have been overridden.
func (c *Config[T]) GetConflicts() map[string]*sourcetrack.SourceInfo {
	return c.sourceTracker.GetConflicts()
}

// GetSourceStatistics returns aggregated source statistics.
func (c *Config[T]) GetSourceStatistics() map[string]any {
	return c.sourceTracker.GetSourceStatistics()
}

// FindKeysFromSource returns keys from sources matching the pattern.
func (c *Config[T]) FindKeysFromSource(pattern string) []string {
	return c.sourceTracker.FindKeysFromSource(pattern)
}

// PrintDebugInfo returns formatted debug info for a key (or all keys if empty).
func (c *Config[T]) PrintDebugInfo(keyPath string) string {
	return c.sourceTracker.PrintDebugInfo(keyPath)
}

// ExportDebugReport writes a full debug report as JSON.
func (c *Config[T]) ExportDebugReport(outputPath string) error {
	return c.sourceTracker.ExportDebugReport(outputPath)
}

// SourceTracker returns the source tracker for advanced inspection.
func (c *Config[T]) SourceTracker() *sourcetrack.Tracker {
	return c.sourceTracker
}

// GenerateDocs generates configuration documentation in the given format ("markdown" or "json").
func (c *Config[T]) GenerateDocs(format string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	flat := dictutil.Flatten(c.envConfig)
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type docEntry struct {
		Key          string `json:"key"`
		Type         string `json:"type"`
		CurrentValue any    `json:"current_value"`
		Source       string `json:"source"`
	}

	var entries []docEntry
	for _, k := range keys {
		v := flat[k]
		source := ""
		if info := c.sourceTracker.GetSourceInfo(k); info != nil {
			source = info.SourceFile
		}
		entries = append(entries, docEntry{
			Key: k, Type: fmt.Sprintf("%T", v), CurrentValue: v, Source: source,
		})
	}

	switch format {
	case "json":
		data, err := json.MarshalIndent(entries, "", "  ")
		return string(data), err

	case "markdown":
		var b strings.Builder
		b.WriteString("| Key | Type | Value | Source |\n")
		b.WriteString("|-----|------|-------|--------|\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "| `%s` | %s | `%v` | %s |\n", e.Key, e.Type, e.CurrentValue, e.Source)
		}
		return b.String(), nil

	default:
		return "", fmt.Errorf("unsupported docs format: %s (use \"markdown\" or \"json\")", format)
	}
}

// Export serializes the config to the given format ("json", "yaml", "toml").
// If outputPath is provided, also writes to that file.
//
// Export is equivalent to [Config.ExportCtx] with [context.Background].
// Hook errors raised by context-aware hooks are propagated to the
// caller and no file write is attempted.
//
// G11: prior to Wave 8 Export marshaled the raw `c.envConfig` without
// applying hooks, leaking unresolved `${secret:…}` placeholders into
// exported artifacts. Export now applies the hook pipeline to a deep
// copy of the config before serializing, so exported JSON/YAML/TOML
// reflects the same effective values seen by [Config.Get] and
// [Config.Typed].
//
// G10 (Wave 12): Export takes its deep-copy snapshot while c.mu.RLock
// is held and releases the lock once the snapshot is owned privately.
// Hooks and marshaling then run against the private snapshot rather
// than against live envConfig, which closes the historical
// "unlocks-before-marshaling" race that let a concurrent Set tear the
// JSON/YAML output. The lock is released BEFORE hooks and marshaling so
// slow or re-entrant user code cannot block or deadlock writers.
func (c *Config[T]) Export(format string, outputPath ...string) ([]byte, error) {
	return c.ExportCtx(context.Background(), format, outputPath...)
}

// ExportCtx serializes the config to the given format ("json", "yaml",
// "toml"), threading ctx through any registered context-aware hooks.
// If outputPath is provided, also writes to that file.
//
// Hook errors raised by context-aware hooks are propagated to the
// caller and no serialization or file write is attempted.
func (c *Config[T]) ExportCtx(ctx context.Context, format string, outputPath ...string) ([]byte, error) {
	data, err := c.ToDictCtx(ctx)
	if err != nil {
		return nil, err
	}

	var result []byte

	switch format {
	case "json":
		result, err = json.MarshalIndent(data, "", "  ")
	case "yaml":
		result, err = yaml.Marshal(data)
	case "toml":
		var buf strings.Builder
		enc := toml.NewEncoder(&buf)
		err = enc.Encode(data)
		result = []byte(buf.String())
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
	if err != nil {
		return nil, err
	}

	if len(outputPath) > 0 && outputPath[0] != "" {
		if err := os.WriteFile(outputPath[0], result, 0600); err != nil {
			return result, err
		}
	}

	return result, nil
}
