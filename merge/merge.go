// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package merge provides configuration merging strategies.
package merge

// Strategy defines how two configuration maps are combined.
type Strategy int

const (
	// Replace overwrites the base entirely with the overlay. At every
	// level a Replace strategy governs, base-only keys are discarded
	// and only overlay keys survive. Per-path strategy overrides on
	// individual shared keys still apply, so "default Replace, but
	// DeepMerge under path X" remains expressible.
	Replace Strategy = iota
	// ShallowMerge preserves top-level base keys while overlay values replace
	// shared keys without recursively combining nested maps.
	ShallowMerge
	// DeepMergeStrategy recursively merges nested maps; non-map type
	// mismatches at a given key fall back to Replace.
	DeepMergeStrategy
	// Append appends overlay items after base items. Non-list operands are
	// treated as one-element lists.
	Append
	// Prepend prepends overlay items before base items. Non-list operands are
	// treated as one-element lists.
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

// Merger combines base with a higher-precedence overlay. Implementations must
// document whether returned maps alias either input; Confii's built-in mergers
// return independent map structure.
type Merger interface {
	Merge(base, overlay map[string]any) map[string]any
}

// MergeAll merges configs from lowest to highest precedence. Nil configurations
// are skipped, and no inputs returns a new empty map. merger must be non-nil
// when two or more non-nil configurations require combination. Whether the
// result aliases an input depends on the Merger implementation; in particular,
// a single input is returned directly.
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
