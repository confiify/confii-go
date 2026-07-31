// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/sourcetrack"
)

const redactedSecretValue = "[REDACTED: secret-backed value]"

// Explain returns source and override metadata for keyPath. For an existing
// key the result contains exists, key, value, current_value, source,
// loader_type, environment, override_count, and—when recorded—
// override_history. For a missing key it contains exists=false, key, and the
// available_keys inventory.
//
// Values originating from secret references are replaced with a redaction
// marker. The returned map and nested values are independent copies and may be
// safely retained or modified by the caller.
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
	secretBacked := c.secretBackedPathLocked(keyPath)
	if secretBacked {
		result["value"] = redactedSecretValue
	}

	if len(info.History) > 0 {
		history := make([]map[string]any, 0, len(info.History))
		for _, h := range info.History {
			historyValue := dictutil.DeepCopyValue(h.Value)
			if secretBacked {
				historyValue = redactedSecretValue
			}
			history = append(history, map[string]any{
				"value":       historyValue,
				"source":      h.Source,
				"loader_type": h.LoaderType,
			})
		}
		result["override_history"] = history
	}

	// Current value from live config — deep-copied so a caller mutating
	// the returned map cannot leak into envConfig.
	if val, ok := dictutil.GetNested(c.envConfig, keyPath); ok {
		if secretBacked {
			result["current_value"] = redactedSecretValue
		} else {
			result["current_value"] = dictutil.DeepCopyValue(val)
		}
	}

	return result
}

// Schema reports the runtime shape of keyPath. The result always includes key
// and exists; an existing key also includes type and value. Secret-backed
// values are redacted. Returned maps and nested values are independent copies.
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
	if c.secretBackedPathLocked(keyPath) {
		result["value"] = redactedSecretValue
	} else {
		result["value"] = dictutil.DeepCopyValue(val)
	}
	result["type"] = fmt.Sprintf("%T", val)

	return result
}

// Layers returns the distinct configured sources in precedence order. Each
// entry contains source, loader_type, keys, and key_count. Duplicate source
// identifiers are represented once. The returned slice, maps, and key lists
// are independent copies.
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
		// Return independent key slices so callers cannot mutate subsequent
		// inspection results.
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

// SecretProvider returns the normalized name of the secret provider declared
// in the active self-configuration. It intentionally exposes no credentials,
// endpoint, namespace, secret path, or resolved value. An empty string means
// that no declarative provider was configured (an explicit custom hook may
// still resolve secrets).
func (c *Config[T]) SecretProvider() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.opts.selfConfigSecretProvider
}

// SecretProviders returns the configured declarative provider aliases in
// deterministic order. It exposes no credentials or backend options.
func (c *Config[T]) SecretProviders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.opts.selfConfigSecretProviders...)
}

// SecretReferenceKeys returns configuration key paths whose raw values
// contain at least one ${secret:...} or ${secret@provider:...} placeholder.
// The referenced provider alias and secret key are never returned, which makes
// this suitable for health reporting.
func (c *Config[T]) SecretReferenceKeys() []string {
	c.mu.RLock()
	snapshot := dictutil.DeepCopy(c.unresolvedEnvConfig)
	c.mu.RUnlock()

	seen := make(map[string]struct{})
	collectSecretReferenceKeys("", snapshot, seen)
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (c *Config[T]) secretBackedPathLocked(keyPath string) bool {
	if c.unresolvedEnvConfig == nil {
		return false
	}
	value, ok := dictutil.GetNested(c.unresolvedEnvConfig, keyPath)
	if !ok {
		return false
	}
	found := make(map[string]struct{})
	collectSecretReferenceKeys(keyPath, value, found)
	return len(found) > 0
}

// SecretReferenceProviders returns the provider aliases selected by raw
// secret references in the active merged configuration. Unqualified
// references contribute the effective default provider; explicitly qualified
// references contribute their alias. Secret keys and resolved values are
// never exposed.
func (c *Config[T]) SecretReferenceProviders() []string {
	return c.SecretReferenceProvidersFor()
}

// SecretReferenceProvidersFor is the key-scoped form of
// [Config.SecretReferenceProviders]. A selected parent path includes every
// reference in its subtree. With no paths it inspects the complete active
// configuration.
func (c *Config[T]) SecretReferenceProvidersFor(keyPaths ...string) []string {
	c.mu.RLock()
	snapshot := dictutil.DeepCopy(c.unresolvedEnvConfig)
	defaultProvider := c.opts.selfConfigSecretProvider
	c.mu.RUnlock()

	seen := make(map[string]struct{})
	if len(keyPaths) == 0 {
		collectSecretReferenceProviders(snapshot, defaultProvider, seen)
	} else {
		for _, keyPath := range keyPaths {
			if value, ok := dictutil.GetNested(snapshot, keyPath); ok {
				collectSecretReferenceProviders(value, defaultProvider, seen)
			}
		}
	}
	providers := make([]string, 0, len(seen))
	for provider := range seen {
		if provider != "" {
			providers = append(providers, provider)
		}
	}
	sort.Strings(providers)
	return providers
}

func collectSecretReferenceKeys(path string, value any, found map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			collectSecretReferenceKeys(childPath, child, found)
		}
	case []any:
		for _, child := range typed {
			collectSecretReferenceKeys(path, child, found)
		}
	case string:
		if path != "" && selfConfigSecretPattern.MatchString(typed) {
			found[path] = struct{}{}
		}
	}
}

func collectSecretReferenceProviders(value any, defaultProvider string, found map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			collectSecretReferenceProviders(child, defaultProvider, found)
		}
	case []any:
		for _, child := range typed {
			collectSecretReferenceProviders(child, defaultProvider, found)
		}
	case string:
		for _, groups := range selfConfigSecretPattern.FindAllStringSubmatch(typed, -1) {
			provider := strings.ToLower(groups[1])
			if provider == "" {
				provider = defaultProvider
			}
			if provider != "" {
				found[provider] = struct{}{}
			}
		}
	}
}

// GetSourceInfo returns an independent copy of the source metadata for keyPath,
// or nil when the key has not been tracked.
func (c *Config[T]) GetSourceInfo(keyPath string) *sourcetrack.SourceInfo {
	return c.sourceTracker.GetSourceInfo(keyPath)
}

// GetOverrideHistory returns the earlier values and sources recorded for
// keyPath, oldest first. It returns nil when no history is available. Complete
// history requires [WithDebugMode](true); returned entries are independent
// copies.
func (c *Config[T]) GetOverrideHistory(keyPath string) []sourcetrack.OverrideEntry {
	return c.sourceTracker.GetOverrideHistory(keyPath)
}

// GetConflicts returns independently copied source records for keys whose
// values were written more than once. An empty map means no overrides were
// observed.
func (c *Config[T]) GetConflicts() map[string]*sourcetrack.SourceInfo {
	return c.sourceTracker.GetConflicts()
}

// GetSourceStatistics returns total_keys, total_overrides, and counts grouped
// under sources and loader_types. The returned maps are independent copies.
func (c *Config[T]) GetSourceStatistics() map[string]any {
	return c.sourceTracker.GetSourceStatistics()
}

// FindKeysFromSource returns keys whose source identifier contains pattern.
// Matching is case-sensitive and the returned slice may be empty.
func (c *Config[T]) FindKeysFromSource(pattern string) []string {
	return c.sourceTracker.FindKeysFromSource(pattern)
}

// PrintDebugInfo returns a human-readable source report for keyPath. An empty
// keyPath includes every tracked key; an unknown key produces a not-found line.
// The report can contain configuration values, including resolved values, and
// must therefore be handled as sensitive operational output.
func (c *Config[T]) PrintDebugInfo(keyPath string) string {
	return c.sourceTracker.PrintDebugInfo(keyPath)
}

// ExportDebugReport writes all tracked source records and override histories as
// indented JSON; newly created files use mode 0600. The report can contain
// configuration values, including resolved values, and must be stored and
// transmitted as sensitive data. Filesystem and encoding failures are returned.
func (c *Config[T]) ExportDebugReport(outputPath string) error {
	return c.sourceTracker.ExportDebugReport(outputPath)
}

// SourceTracker returns the Config's concurrency-safe source tracker. Callers
// may use its read methods for advanced inspection; mutating methods affect
// subsequent introspection and should normally be left to Config.
func (c *Config[T]) SourceTracker() *sourcetrack.Tracker {
	return c.sourceTracker
}

// GenerateDocs renders the current key inventory as "markdown" or "json".
// Entries contain the key, Go type, current value, and source. Secret-backed
// values are redacted. The format name is case-sensitive; unsupported formats
// return an error.
func (c *Config[T]) GenerateDocs(format string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	flat := dictutil.Flatten(c.envConfig)
	raw := c.unresolvedEnvConfig
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
		if rawValue, ok := dictutil.GetNested(raw, k); ok {
			found := make(map[string]struct{})
			collectSecretReferenceKeys(k, rawValue, found)
			if len(found) > 0 {
				v = redactedSecretValue
			}
		}
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

// Export serializes the effective snapshot using the exporter registered for
// format. JSON, YAML, and TOML are registered by default; [WithExporter] can
// replace them or add application-defined formats. The returned bytes always
// contain the serialized result. When a non-empty outputPath is supplied, the
// first path is also written; newly created files use mode 0600.
//
// Export uses the implicit runtime context bounded by [WithOperationTimeout].
// Export serializes resolved values, including secrets. Treat the bytes and any
// output file as sensitive. Unsupported formats, serialization failures, and
// file-write failures are returned; a write failure may be accompanied by the
// successfully serialized bytes.
func (c *Config[T]) Export(format string, outputPath ...string) ([]byte, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.ExportWithContext(ctx, format, outputPath...)
}

// ExportWithContext is the context-aware form of [Config.Export]. A nil or
// canceled context is returned before serialization. The method exports the
// already-materialized snapshot and therefore performs no provider I/O or hook
// execution during a successful read. Resolved secret values are not redacted.
func (c *Config[T]) ExportWithContext(ctx context.Context, format string, outputPath ...string) ([]byte, error) {
	data, err := c.ToDictWithContext(ctx)
	if err != nil {
		return nil, err
	}

	exporter, ok := c.exporters[format]
	if !ok {
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
	result, err := exporter.Export(data)
	if err != nil {
		return nil, fmt.Errorf("export %s: %w", format, err)
	}

	if len(outputPath) > 0 && outputPath[0] != "" {
		if err := os.WriteFile(outputPath[0], result, 0600); err != nil {
			return result, err
		}
	}

	return result, nil
}
