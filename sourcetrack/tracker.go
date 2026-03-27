// Package sourcetrack provides per-key source tracking for configuration values,
// recording where each value originated, how many times it was overridden, and
// the full override history.
package sourcetrack

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// SourceInfo records the origin and override history of a configuration key.
type SourceInfo struct {
	Key           string          `json:"key"`
	Value         any             `json:"value"`
	SourceFile    string          `json:"source_file"`
	LoaderType    string          `json:"loader_type"`
	LineNumber    int             `json:"line_number,omitempty"`
	Environment   string          `json:"environment,omitempty"`
	OverrideCount int             `json:"override_count"`
	History       []OverrideEntry `json:"history,omitempty"`
	Timestamp     time.Time       `json:"timestamp"`
}

// OverrideEntry records a single override event.
type OverrideEntry struct {
	Value      any    `json:"value"`
	Source     string `json:"source"`
	LoaderType string `json:"loader_type"`
}

// Tracker tracks the source and override history of configuration keys.
type Tracker struct {
	mu        sync.RWMutex
	sources   map[string]*SourceInfo
	debugMode bool
}

// NewTracker creates a new source tracker.
func NewTracker(debugMode bool) *Tracker {
	return &Tracker{
		sources:   make(map[string]*SourceInfo),
		debugMode: debugMode,
	}
}

// TrackValue records the source of a configuration value.
func (t *Tracker) TrackValue(key string, value any, sourceFile, loaderType, environment string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	existing, ok := t.sources[key]
	if ok {
		// Override: record history.
		existing.OverrideCount++
		if t.debugMode {
			existing.History = append(existing.History, OverrideEntry{
				Value:      existing.Value,
				Source:     existing.SourceFile,
				LoaderType: existing.LoaderType,
			})
		}
		existing.Value = value
		existing.SourceFile = sourceFile
		existing.LoaderType = loaderType
		existing.Timestamp = time.Now()
	} else {
		t.sources[key] = &SourceInfo{
			Key:         key,
			Value:       value,
			SourceFile:  sourceFile,
			LoaderType:  loaderType,
			Environment: environment,
			Timestamp:   time.Now(),
		}
	}
}

// TrackConfig recursively tracks all keys in a config map.
func (t *Tracker) TrackConfig(config map[string]any, sourceFile, loaderType, environment, prefix string) {
	for k, v := range config {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		if m, ok := v.(map[string]any); ok {
			t.TrackConfig(m, sourceFile, loaderType, environment, fullKey)
		} else {
			t.TrackValue(fullKey, v, sourceFile, loaderType, environment)
		}
	}
}

// GetSourceInfo returns the source info for a key.
func (t *Tracker) GetSourceInfo(key string) *SourceInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sources[key]
}

// GetOverrideHistory returns the override history for a key.
func (t *Tracker) GetOverrideHistory(key string) []OverrideEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	info := t.sources[key]
	if info == nil {
		return nil
	}
	return info.History
}

// GetConflicts returns all keys that have been overridden at least once.
func (t *Tracker) GetConflicts() map[string]*SourceInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	conflicts := make(map[string]*SourceInfo)
	for k, info := range t.sources {
		if info.OverrideCount > 0 {
			conflicts[k] = info
		}
	}
	return conflicts
}

// FindKeysFromSource returns keys that originated from sources matching the pattern.
func (t *Tracker) FindKeysFromSource(pattern string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var keys []string
	for k, info := range t.sources {
		if strings.Contains(info.SourceFile, pattern) {
			keys = append(keys, k)
		}
	}
	return keys
}

// GetSourceStatistics returns aggregated statistics about sources.
func (t *Tracker) GetSourceStatistics() map[string]any {
	t.mu.RLock()
	defer t.mu.RUnlock()
	sourceCounts := make(map[string]int)
	loaderCounts := make(map[string]int)
	totalOverrides := 0
	for _, info := range t.sources {
		sourceCounts[info.SourceFile]++
		loaderCounts[info.LoaderType]++
		totalOverrides += info.OverrideCount
	}
	return map[string]any{
		"total_keys":      len(t.sources),
		"sources":         sourceCounts,
		"loader_types":    loaderCounts,
		"total_overrides": totalOverrides,
	}
}

// ExportDebugReport writes a full debug report as JSON.
func (t *Tracker) ExportDebugReport(outputPath string) error {
	t.mu.RLock()
	report := make(map[string]any)
	for k, info := range t.sources {
		report[k] = info
	}
	t.mu.RUnlock()

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, data, 0644)
}

// PrintDebugInfo prints debug info for a key (or all keys if key is empty) to stdout.
func (t *Tracker) PrintDebugInfo(key string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var b strings.Builder
	if key != "" {
		info := t.sources[key]
		if info == nil {
			fmt.Fprintf(&b, "Key %q not found\n", key)
			return b.String()
		}
		printSourceInfo(&b, info)
	} else {
		for _, info := range t.sources {
			printSourceInfo(&b, info)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func printSourceInfo(b *strings.Builder, info *SourceInfo) {
	fmt.Fprintf(b, "Key:       %s\n", info.Key)
	fmt.Fprintf(b, "Value:     %v\n", info.Value)
	fmt.Fprintf(b, "Source:    %s\n", info.SourceFile)
	fmt.Fprintf(b, "Loader:    %s\n", info.LoaderType)
	fmt.Fprintf(b, "Overrides: %d\n", info.OverrideCount)
	if len(info.History) > 0 {
		fmt.Fprintf(b, "History:\n")
		for i, h := range info.History {
			fmt.Fprintf(b, "  %d. %v (from %s via %s)\n", i+1, h.Value, h.Source, h.LoaderType)
		}
	}
}
