package dictutil

import "strings"

// Flatten converts a nested map into a flat map with dot-separated keys.
// Only leaf values (non-map values) are included.
//
// Example: {"database": {"host": "localhost"}} → {"database.host": "localhost"}
func Flatten(data map[string]any) map[string]any {
	result := make(map[string]any)
	flatten("", data, result)
	return result
}

func flatten(prefix string, data map[string]any, result map[string]any) {
	for k, v := range data {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if m, ok := v.(map[string]any); ok {
			flatten(key, m, result)
		} else {
			result[key] = v
		}
	}
}

// FlatKeys returns all dot-separated leaf key paths from a nested map.
func FlatKeys(data map[string]any) []string {
	flat := Flatten(data)
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	return keys
}

// FlatKeysWithPrefix returns flat keys that start with the given prefix,
// with the prefix stripped from each key.
func FlatKeysWithPrefix(data map[string]any, prefix string) []string {
	all := FlatKeys(data)
	if prefix == "" {
		return all
	}
	prefix = prefix + "."
	var result []string
	for _, k := range all {
		if after, ok := strings.CutPrefix(k, prefix); ok {
			result = append(result, after)
		}
	}
	return result
}

// Unflatten converts a flat map with dot-separated keys into a nested map.
//
// Example: {"database.host": "localhost"} → {"database": {"host": "localhost"}}
func Unflatten(data map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range data {
		_ = SetNested(result, k, v)
	}
	return result
}
