// Package dictutil provides utility functions for working with nested
// map[string]any configuration dictionaries.
package dictutil

import (
	"maps"
	"strings"
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
	parts := strings.Split(keyPath, ".")
	current := any(data)

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// SetNested sets a value in a nested map using a dot-separated key path.
// Intermediate maps are created as needed.
// Returns an error if an intermediate value exists but is not a map.
func SetNested(data map[string]any, keyPath string, value any) error {
	parts := strings.Split(keyPath, ".")
	current := data

	for i, part := range parts[:len(parts)-1] {
		next, ok := current[part]
		if !ok {
			// Create intermediate map.
			m := make(map[string]any)
			current[part] = m
			current = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return &PathError{
				Path:    strings.Join(parts[:i+1], "."),
				Message: "intermediate value is not a map",
			}
		}
		current = m
	}

	current[parts[len(parts)-1]] = value
	return nil
}

// HasNested checks if a key path exists in the nested map.
func HasNested(data map[string]any, keyPath string) bool {
	_, ok := GetNested(data, keyPath)
	return ok
}

// PathError is returned when a key path operation encounters a non-map intermediate.
type PathError struct {
	Path    string
	Message string
}

// Error returns a human-readable description of the path error.
func (e *PathError) Error() string {
	return "path " + e.Path + ": " + e.Message
}
