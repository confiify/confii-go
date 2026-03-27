// Package merge provides configuration merging strategies.
package merge

import "github.com/confiify/confii-go/internal/dictutil"

// Strategy defines how two configuration maps are combined.
type Strategy int

const (
	// Replace overwrites the base value entirely with the overlay value.
	Replace Strategy = iota
	// Merge deep-merges nested maps; type mismatches fall back to replace.
	DeepMergeStrategy
	// Append appends overlay list items after base list items.
	Append
	// Prepend prepends overlay list items before base list items.
	Prepend
	// Intersection keeps only keys present in both configs.
	Intersection
	// Union keeps all keys from both configs, merging common keys.
	Union
)

// Merger combines two configuration maps.
type Merger interface {
	Merge(base, overlay map[string]any) map[string]any
}

// DefaultMerger merges configurations using shallow or deep merge.
type DefaultMerger struct {
	DeepMerge bool
}

// NewDefault creates a DefaultMerger. When deepMerge is true, nested maps
// are recursively merged; otherwise top-level keys are replaced.
func NewDefault(deepMerge bool) *DefaultMerger {
	return &DefaultMerger{DeepMerge: deepMerge}
}

func (m *DefaultMerger) Merge(base, overlay map[string]any) map[string]any {
	if m.DeepMerge {
		return dictutil.DeepMerge(base, overlay)
	}
	return dictutil.ShallowMerge(base, overlay)
}

// MergeAll merges multiple configurations in order using the given merger.
// Later configs override earlier ones.
func MergeAll(merger Merger, configs ...map[string]any) map[string]any {
	if len(configs) == 0 {
		return make(map[string]any)
	}
	result := configs[0]
	if result == nil {
		result = make(map[string]any)
	}
	for _, cfg := range configs[1:] {
		if cfg != nil {
			result = merger.Merge(result, cfg)
		}
	}
	return result
}
