package merge

import (
	"maps"
	"strings"
)

// AdvancedMerger supports per-path merge strategy overrides.
//
// The default strategy is applied at every node where a more specific
// per-path entry does not match. The semantics for each strategy are:
//
//   - Replace: at every level it governs, discards base-only keys and
//     keeps only the overlay's keys. Per-path overrides on individual
//     shared keys still apply, so a configuration that uses Replace as
//     its default but DeepMerge on a specific path will still recurse
//     into that path.
//   - DeepMergeStrategy: recursively merges nested maps; non-map type
//     mismatches fall back to Replace.
//   - Union: same shape as DeepMergeStrategy but all base-only and
//     overlay-only keys are preserved (union of key sets); recursion
//     routes through mergeAt so nested per-path strategy overrides
//     resolve correctly. Previous implementations called the package
//     level deep-merge directly and ignored the StrategyMap below the
//     Union node.
//   - Intersection: keeps only keys present in both maps with equal
//     values; unequal scalar values and type mismatches are omitted
//     entirely (not preserved as nil placeholders).
//   - Append / Prepend: appends/prepends overlay items relative to the
//     base list. Non-list operands are treated as single-element lists,
//     making the strategy useful while a source evolves from one value to
//     several values.
type AdvancedMerger struct {
	DefaultStrategy Strategy
	StrategyMap     map[string]Strategy // dot-separated path → strategy
}

// NewAdvanced creates an AdvancedMerger with the given default strategy.
func NewAdvanced(defaultStrategy Strategy, strategyMap map[string]Strategy) *AdvancedMerger {
	if strategyMap == nil {
		strategyMap = make(map[string]Strategy)
	}
	return &AdvancedMerger{
		DefaultStrategy: defaultStrategy,
		StrategyMap:     strategyMap,
	}
}

// Merge combines base and overlay using the merger's default strategy
// and any per-path overrides registered in StrategyMap.
func (m *AdvancedMerger) Merge(base, overlay map[string]any) map[string]any {
	return m.mergeAt(base, overlay, "")
}

func (m *AdvancedMerger) mergeAt(base, overlay map[string]any, prefix string) map[string]any {
	// Resolve the strategy that governs this map level.
	topStrategy := m.resolveStrategy(prefix)

	if topStrategy == Intersection {
		return m.intersectMaps(base, overlay, prefix)
	}

	// Initial result depends on whether this level discards base-only
	// keys (Replace) or preserves them (every other strategy, including
	// the implicit Union/DeepMerge behavior at non-overridden paths).
	var result map[string]any
	if topStrategy == Replace {
		// Replace at this level: drop every base-only key. Per-path
		// strategy overrides on shared keys still take effect through
		// applyStrategy below, which is what allows configurations like
		// "default Replace + DeepMerge on path X" to keep working.
		result = make(map[string]any, len(overlay))
	} else {
		result = make(map[string]any, len(base))
		maps.Copy(result, base)
	}

	for k, overlayVal := range overlay {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		strategy := m.resolveStrategy(path)
		baseVal, exists := base[k]

		if !exists {
			// New key from overlay — accepted under every non-Intersection
			// strategy (Intersection took the early return above).
			result[k] = overlayVal
			continue
		}

		result[k] = m.applyStrategy(strategy, baseVal, overlayVal, path)
	}

	return result
}

func (m *AdvancedMerger) intersectMaps(base, overlay map[string]any, prefix string) map[string]any {
	result := make(map[string]any)
	for k, bv := range base {
		ov, ok := overlay[k]
		if !ok {
			continue // only in base, skip
		}
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		val, keep := m.intersectValues(bv, ov, path)
		if !keep {
			// Scalar mismatch or empty nested intersection — omit the
			// key entirely instead of emitting a nil placeholder.
			continue
		}
		result[k] = val
	}
	return result
}

func (m *AdvancedMerger) resolveStrategy(path string) Strategy {
	// Exact path match.
	if s, ok := m.StrategyMap[path]; ok {
		return s
	}
	// Most specific parent path match.
	best := ""
	bestStrategy := m.DefaultStrategy
	for p, s := range m.StrategyMap {
		if strings.HasPrefix(path, p+".") && len(p) > len(best) {
			best = p
			bestStrategy = s
		}
	}
	if best != "" {
		return bestStrategy
	}
	return m.DefaultStrategy
}

func (m *AdvancedMerger) applyStrategy(strategy Strategy, baseVal, overlayVal any, path string) any {
	switch strategy {
	case Replace:
		return overlayVal

	case DeepMergeStrategy:
		baseMap, baseOk := baseVal.(map[string]any)
		overlayMap, overlayOk := overlayVal.(map[string]any)
		if baseOk && overlayOk {
			return m.mergeAt(baseMap, overlayMap, path)
		}
		return overlayVal

	case Append:
		return appendLists(baseVal, overlayVal)

	case Prepend:
		return prependLists(baseVal, overlayVal)

	case Intersection:
		// Recursive intersection at a per-key level — return nil when
		// the values do not intersect. The caller (intersectMaps) is
		// responsible for translating nil into key omission.
		return intersect(baseVal, overlayVal)

	case Union:
		// Union recursion routes through mergeAt so nested per-path
		// strategy overrides resolve correctly. Previous implementation
		// called dictutil.DeepMerge directly and silently bypassed the
		// strategy map for everything below the Union node.
		baseMap, baseOk := baseVal.(map[string]any)
		overlayMap, overlayOk := overlayVal.(map[string]any)
		if baseOk && overlayOk {
			return m.mergeAt(baseMap, overlayMap, path)
		}
		return overlayVal

	default:
		return overlayVal
	}
}

// intersectValues returns (value, true) if base and overlay agree at
// this position, or (nil, false) if the value should be omitted from
// the parent intersection. Recursion preserves the merger's StrategyMap
// for nested intersections.
func (m *AdvancedMerger) intersectValues(base, overlay any, path string) (any, bool) {
	baseMap, baseOk := base.(map[string]any)
	overlayMap, overlayOk := overlay.(map[string]any)
	if baseOk && overlayOk {
		nested := m.intersectMaps(baseMap, overlayMap, path)
		if len(nested) == 0 {
			return nil, false
		}
		return nested, true
	}
	if baseOk != overlayOk {
		// Type mismatch (one side map, one side scalar) — drop.
		return nil, false
	}
	if base == overlay {
		return base, true
	}
	return nil, false
}

// appendLists appends overlay's list elements after base's list elements.
// Non-list operands are wrapped as one-element lists.
func appendLists(base, overlay any) any {
	baseList := toSlice(base)
	overlayList := toSlice(overlay)
	merged := make([]any, 0, len(baseList)+len(overlayList))
	merged = append(merged, baseList...)
	merged = append(merged, overlayList...)
	return merged
}

// prependLists prepends overlay's list elements before base's list elements.
// Non-list operands are wrapped as one-element lists.
func prependLists(base, overlay any) any {
	baseList := toSlice(base)
	overlayList := toSlice(overlay)
	merged := make([]any, 0, len(overlayList)+len(baseList))
	merged = append(merged, overlayList...)
	merged = append(merged, baseList...)
	return merged
}

// intersect computes the deep intersection of two values. Used as a
// helper from applyStrategy for per-key Intersection semantics. A nil
// result signals "no intersection" — the caller decides whether to
// omit the key (intersectMaps does) or surface the nil. This helper
// does not consult the merger's StrategyMap; per-path resolution is
// handled by intersectMaps via intersectValues.
func intersect(base, overlay any) any {
	baseMap, baseOk := base.(map[string]any)
	overlayMap, overlayOk := overlay.(map[string]any)
	if !baseOk || !overlayOk {
		if base == overlay {
			return base
		}
		return nil
	}

	result := make(map[string]any)
	for k, bv := range baseMap {
		if ov, ok := overlayMap[k]; ok {
			bm, bmOk := bv.(map[string]any)
			om, omOk := ov.(map[string]any)
			if bmOk && omOk {
				nested := intersect(bm, om)
				if nested != nil {
					result[k] = nested
				}
			} else if bv == ov {
				result[k] = bv
			}
			// Unequal scalars are omitted, not retained as nil.
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// toSlice converts a merge operand into the list representation used by
// Append and Prepend.
func toSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{v}
}

// Compile-time assertion that AdvancedMerger satisfies the Merger
// interface declared in merge.go.
var _ Merger = (*AdvancedMerger)(nil)
