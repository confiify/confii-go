// Package merge provides configuration merging strategies.
package merge

import "github.com/confiify/confii-go/internal/dictutil"

// Strategy defines how two configuration maps are combined.
type Strategy int

const (
	// Replace overwrites the base entirely with the overlay. At every
	// level a Replace strategy governs, base-only keys are discarded
	// and only overlay keys survive. Per-path strategy overrides on
	// individual shared keys still apply, so "default Replace, but
	// DeepMerge under path X" remains expressible.
	Replace Strategy = iota
	// DeepMergeStrategy recursively merges nested maps; non-map type
	// mismatches at a given key fall back to Replace.
	DeepMergeStrategy
	// Append appends overlay list items after base list items. On any
	// type mismatch (either side is not a list) Append falls back to
	// Replace and returns the overlay verbatim instead of silently
	// coercing scalars to single-element lists.
	Append
	// Prepend prepends overlay list items before base list items.
	// Type-mismatch behavior matches Append.
	Prepend
	// Intersection keeps only keys present in both configs whose values
	// are equal scalars or whose nested maps have a non-empty
	// intersection. Unequal scalars are omitted (not retained as nil).
	Intersection
	// Union keeps all keys from both configs, merging common keys.
	// Recursion routes through the per-path strategy resolver so nested
	// strategy overrides take effect under a Union root.
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

// Merge combines base and overlay configuration maps using the configured strategy.
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
