package merge

import (
	"maps"
	"strings"

	"github.com/qualitycoe/confii-go/internal/dictutil"
)

// AdvancedMerger supports per-path merge strategy overrides.
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

func (m *AdvancedMerger) Merge(base, overlay map[string]any) map[string]any {
	return m.mergeAt(base, overlay, "")
}

func (m *AdvancedMerger) mergeAt(base, overlay map[string]any, prefix string) map[string]any {
	// For top-level intersection, we need to determine the strategy first.
	topStrategy := m.resolveStrategy(prefix)

	if topStrategy == Intersection {
		return m.intersectMaps(base, overlay, prefix)
	}

	result := make(map[string]any, len(base))
	maps.Copy(result, base)

	for k, overlayVal := range overlay {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		strategy := m.resolveStrategy(path)
		baseVal, exists := result[k]

		if !exists {
			// For union, add; for other strategies, add new keys.
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
		result[k] = m.applyStrategy(Intersection, bv, ov, path)
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
		return intersect(baseVal, overlayVal)

	case Union:
		baseMap, baseOk := baseVal.(map[string]any)
		overlayMap, overlayOk := overlayVal.(map[string]any)
		if baseOk && overlayOk {
			return dictutil.DeepMerge(baseMap, overlayMap)
		}
		return overlayVal

	default:
		return overlayVal
	}
}

func appendLists(base, overlay any) any {
	baseList := toSlice(base)
	overlayList := toSlice(overlay)
	return append(baseList, overlayList...)
}

func prependLists(base, overlay any) any {
	baseList := toSlice(base)
	overlayList := toSlice(overlay)
	return append(overlayList, baseList...)
}

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
			// Both have this key; recurse for maps, keep if equal.
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
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func toSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return []any{v}
}
