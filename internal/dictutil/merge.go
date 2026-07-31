// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package dictutil provides utility functions for working with nested
// map[string]any configuration dictionaries.
package dictutil

import (
	"maps"

	"github.com/confiify/confii-go/v2/configmap"
)

// DeepMerge recursively merges overlay into base. For any key present in both:
//   - If both values are maps, recurse.
//   - Otherwise, the overlay value replaces the base value.
//
// Returns a new map; neither base nor overlay is modified.
func DeepMerge(base, overlay map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	maps.Copy(result, base)
	for k, v := range overlay {
		if baseVal, ok := result[k]; ok {
			baseMap, baseIsMap := baseVal.(map[string]any)
			overlayMap, overlayIsMap := v.(map[string]any)
			if baseIsMap && overlayIsMap {
				result[k] = DeepMerge(baseMap, overlayMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

// ShallowMerge copies base then applies overlay at the top level only.
func ShallowMerge(base, overlay map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(overlay))
	maps.Copy(result, base)
	maps.Copy(result, overlay)
	return result
}

// GetNested retrieves a value from a nested map using a dot-separated key path.
// Returns (value, true) if found, (nil, false) otherwise.
func GetNested(data map[string]any, keyPath string) (any, bool) {
	return configmap.Get(data, keyPath)
}

// SetNested sets a value in a nested map using a dot-separated key path.
// Intermediate maps are created as needed.
// Returns an error if an intermediate value exists but is not a map.
func SetNested(data map[string]any, keyPath string, value any) error {
	return configmap.Set(data, keyPath, value)
}

// HasNested checks if a key path exists in the nested map.
func HasNested(data map[string]any, keyPath string) bool {
	return configmap.Has(data, keyPath)
}

// PathError aliases the public path error while internal call sites migrate to
// configmap directly.
type PathError = configmap.PathError
