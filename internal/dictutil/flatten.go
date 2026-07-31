// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package dictutil

import "github.com/confiify/confii-go/v2/configmap"

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
	return configmap.Keys(data)
}
