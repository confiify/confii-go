// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package cloudutil contains implementation details shared by cloud loaders.
package cloudutil

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseScalar converts an SSM string to a conservative scalar value.
func ParseScalar(value string) any {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "true":
		return true
	case "false":
		return false
	}
	if i, err := strconv.ParseInt(value, 10, strconv.IntSize); err == nil && strconv.FormatInt(i, 10) == value {
		return int(i)
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil && strings.ContainsAny(value, ".eE") {
		return f
	}
	return value
}

// SetNested sets a dot-separated key while rejecting scalar intermediates.
func SetNested(data map[string]any, keyPath string, value any) error {
	parts := strings.Split(keyPath, ".")
	if len(parts) == 0 || parts[0] == "" {
		return fmt.Errorf("empty key path")
	}
	current := data
	for i, part := range parts[:len(parts)-1] {
		next, ok := current[part]
		if !ok {
			nested := make(map[string]any)
			current[part] = nested
			current = nested
			continue
		}
		nested, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("path %s: intermediate value is not a map", strings.Join(parts[:i+1], "."))
		}
		current = nested
	}
	current[parts[len(parts)-1]] = value
	return nil
}
