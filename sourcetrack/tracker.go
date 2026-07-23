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

	"github.com/confiify/confii-go/internal/dictutil"
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
//
// All read methods on Tracker (e.g. [Tracker.GetSourceInfo],
// [Tracker.GetOverrideHistory], [Tracker.GetConflicts],
// [Tracker.FindKeysFromSource], [Tracker.GetSourceStatistics]) return
// defensive deep copies of internal state. Mutating returned values —
// scalar fields, History slice elements, or map entries — does not
// affect tracker state, and a returned *SourceInfo cannot be
// retroactively corrupted by a concurrent [Tracker.TrackValue],
// [Tracker.Restore], or other tracker mutation. Callers may freely
// mutate, retain, or share returned values without aliasing hazards
// (D06/D08 / F-Tracker-GetConflicts-Aliasing contract).
type Tracker struct {
	mu sync.RWMutex
	// exportMu serializes replacement of a debug report file. Snapshotting is
	// independently concurrent, but two os.WriteFile calls to the same path can
	// otherwise truncate/interleave and leave malformed JSON.
	exportMu  sync.Mutex
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

// cloneSourceInfo returns a deep copy of info or nil if info is nil.
// Both [SourceInfo.Value] and every [OverrideEntry.Value] are routed
// through [dictutil.DeepCopyValue] so a caller mutating a returned
// map or slice payload cannot alias live tracker state through the
// shared reference. Used by every read path on [Tracker] that escapes
// a *SourceInfo to a caller, and by [Tracker.Snapshot] /
// [Tracker.ExportDebugReport] to take a private snapshot before
// releasing the lock.
func cloneSourceInfo(info *SourceInfo) *SourceInfo {
	if info == nil {
		return nil
	}
	clone := *info
	clone.Value = dictutil.DeepCopyValue(info.Value)
	if len(info.History) > 0 {
		history := make([]OverrideEntry, len(info.History))
		for i, h := range info.History {
			history[i] = OverrideEntry{
				Value:      dictutil.DeepCopyValue(h.Value),
				Source:     h.Source,
				LoaderType: h.LoaderType,
			}
		}
		clone.History = history
	}
	return &clone
}

// Snapshot captures an opaque copy of the tracker's current state suitable
// for later [Tracker.Restore]. It is used by callers that need to roll
// back tracker mutations performed during a failed reload (G14 / D05).
//
// The returned value is a deep copy of every recorded [SourceInfo],
// including the per-key override history slice, so subsequent mutations
// on the live tracker do not corrupt the snapshot. The snapshot itself
// is otherwise opaque; callers must not depend on its layout.
func (t *Tracker) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	snap := make(map[string]*SourceInfo, len(t.sources))
	for k, info := range t.sources {
		if info == nil {
			continue
		}
		snap[k] = cloneSourceInfo(info)
	}
	return Snapshot{sources: snap}
}

// Restore replaces the tracker's state with the contents of s. After this
// call, the tracker reports exactly the keys captured at the point
// [Tracker.Snapshot] was taken. Restore is safe to call concurrently with
// other tracker methods; callers in [confii.Config.Reload] use it under
// the Config's write lock.
func (t *Tracker) Restore(s Snapshot) {
	t.mu.Lock()
	defer t.mu.Unlock()
	restored := make(map[string]*SourceInfo, len(s.sources))
	for k, info := range s.sources {
		if info == nil {
			continue
		}
		restored[k] = cloneSourceInfo(info)
	}
	t.sources = restored
}

// Snapshot is an opaque, immutable capture of a [Tracker]'s state. It is
// produced by [Tracker.Snapshot] and consumed by [Tracker.Restore]. The
// zero value is a valid empty snapshot — restoring it clears the tracker.
type Snapshot struct {
	sources map[string]*SourceInfo
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

// GetSourceInfo returns the source info for a key, or nil if no value has
// been tracked for that key.
//
// The returned *SourceInfo is a defensive deep copy of the tracker's
// internal record (including a fresh copy of the History slice).
// Mutations on the returned value do not affect tracker state, and
// concurrent tracker mutations (e.g. a [Tracker.TrackValue] override or a
// [Tracker.Restore] rollback during a failed reload) cannot retroactively
// corrupt a *SourceInfo that a caller is still holding (D06).
func (t *Tracker) GetSourceInfo(key string) *SourceInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return cloneSourceInfo(t.sources[key])
}

// GetOverrideHistory returns the override history for a key.
//
// The returned slice is a defensive deep copy of the tracker's
// internal History. Mutating elements, appending, or mutating any
// entry's .Value payload does not affect tracker state. A concurrent
// [Tracker.TrackValue] override cannot retroactively corrupt a slice
// already returned to a caller.
func (t *Tracker) GetOverrideHistory(key string) []OverrideEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	info := t.sources[key]
	if info == nil {
		return nil
	}
	if len(info.History) == 0 {
		return nil
	}
	out := make([]OverrideEntry, len(info.History))
	for i, h := range info.History {
		out[i] = OverrideEntry{
			Value:      dictutil.DeepCopyValue(h.Value),
			Source:     h.Source,
			LoaderType: h.LoaderType,
		}
	}
	return out
}

// GetConflicts returns all keys that have been overridden at least once.
//
// Each *SourceInfo value in the returned map is a defensive deep copy
// of the tracker's internal record (including a fresh copy of its
// History slice) — mirroring the D06 contract for
// [Tracker.GetSourceInfo]. Mutating the returned map, its *SourceInfo
// values, or their History slices does not affect tracker state, and a
// concurrent [Tracker.TrackValue] / [Tracker.Restore] cannot
// retroactively corrupt values already returned to a caller
// (F-Tracker-GetConflicts-Aliasing).
func (t *Tracker) GetConflicts() map[string]*SourceInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	conflicts := make(map[string]*SourceInfo)
	for k, info := range t.sources {
		if info == nil {
			continue
		}
		if info.OverrideCount > 0 {
			conflicts[k] = cloneSourceInfo(info)
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

// ExportDebugReport writes a full debug report as JSON to outputPath.
//
// The routine takes a deep-copied snapshot under RLock and releases
// the lock before marshaling. Marshaling under the lock would block
// every writer for the duration of the encode; marshaling against
// live pointers would race a concurrent [Tracker.TrackValue] mutating
// SourceInfo fields in place.
func (t *Tracker) ExportDebugReport(outputPath string) error {
	t.mu.RLock()
	snapshot := make(map[string]*SourceInfo, len(t.sources))
	for k, info := range t.sources {
		if info == nil {
			continue
		}
		snapshot[k] = cloneSourceInfo(info)
	}
	t.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	t.exportMu.Lock()
	defer t.exportMu.Unlock()
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
