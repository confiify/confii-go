// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package configmap

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

var (
	// ErrInvalidPath identifies an empty path or a path containing an empty
	// segment.
	ErrInvalidPath = errors.New("invalid configuration path")
	// ErrNilMap identifies an attempt to write through a nil map.
	ErrNilMap = errors.New("nil configuration map")
	// ErrPathConflict identifies a path whose intermediate segment contains a
	// non-map value.
	ErrPathConflict = errors.New("configuration path conflict: intermediate value is not a map")
)

// PathError describes why a key-path mutation failed. Path contains the
// complete requested path and Segment contains the prefix at which the
// failure occurred. Use [errors.Is] with [ErrInvalidPath], [ErrNilMap], or
// [ErrPathConflict] to classify the failure.
type PathError struct {
	Path    string
	Segment string
	Err     error
}

// Error returns a value-safe description of the path failure.
func (e *PathError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Segment == "" || e.Segment == e.Path {
		return fmt.Sprintf("path %q: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("path %q at %q: %v", e.Path, e.Segment, e.Err)
}

// Unwrap returns the stable error category.
func (e *PathError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Get retrieves the value at path. It returns false when data is nil, the path
// is invalid or missing, or an intermediate segment is not a map. Returned map
// and slice values remain owned by data; callers that require isolation must
// copy them.
func Get(data map[string]any, path string) (any, bool) {
	parts, ok := parse(path)
	if !ok || data == nil {
		return nil, false
	}

	current := any(data)
	for _, part := range parts {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// Has reports whether path resolves to a value. A present nil value counts as
// existing.
func Has(data map[string]any, path string) bool {
	_, ok := Get(data, path)
	return ok
}

// Set stores value at path, creating missing intermediate maps. The operation
// is atomic on error: data is not modified when the path is invalid, data is
// nil, or an existing intermediate segment is not a map. Set stores value
// directly; it does not make a defensive copy.
func Set(data map[string]any, path string, value any) error {
	parts, ok := parse(path)
	if !ok {
		return &PathError{Path: path, Segment: invalidSegment(parts), Err: ErrInvalidPath}
	}
	if data == nil {
		return &PathError{Path: path, Err: ErrNilMap}
	}

	current := data
	for index, part := range parts[:len(parts)-1] {
		next, exists := current[part]
		if !exists {
			branch := buildBranch(parts[index+1:], value)
			current[part] = branch
			return nil
		}
		nested, nestedOK := next.(map[string]any)
		if !nestedOK {
			return &PathError{
				Path:    path,
				Segment: strings.Join(parts[:index+1], "."),
				Err:     ErrPathConflict,
			}
		}
		if nested == nil {
			nested = make(map[string]any)
			current[part] = nested
		}
		current = nested
	}

	current[parts[len(parts)-1]] = value
	return nil
}

// Keys returns sorted, fully qualified paths for every leaf value in data.
// With a prefix, only descendants of that path are returned; the prefix is not
// stripped, so every result can be passed directly to [Get], [Has], or [Set].
// Empty maps have no leaf keys. Cyclic map references are safely skipped.
// Only the first optional prefix is used.
func Keys(data map[string]any, prefix ...string) []string {
	if data == nil {
		return []string{}
	}

	all := make([]string, 0)
	collectKeys("", data, make(map[uintptr]bool), &all)

	if len(prefix) > 0 && prefix[0] != "" {
		parts, ok := parse(strings.TrimSuffix(prefix[0], "."))
		if !ok {
			return []string{}
		}
		match := strings.Join(parts, ".") + "."
		filtered := all[:0]
		for _, key := range all {
			if strings.HasPrefix(key, match) {
				filtered = append(filtered, key)
			}
		}
		all = filtered
	}

	sort.Strings(all)
	return all
}

func parse(path string) ([]string, bool) {
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	for _, part := range parts {
		if part == "" {
			return parts, false
		}
	}
	return parts, true
}

func invalidSegment(parts []string) string {
	for index, part := range parts {
		if part == "" {
			return strings.Join(parts[:index+1], ".")
		}
	}
	return ""
}

func buildBranch(parts []string, value any) map[string]any {
	branch := make(map[string]any)
	current := branch
	for _, part := range parts[:len(parts)-1] {
		nested := make(map[string]any)
		current[part] = nested
		current = nested
	}
	current[parts[len(parts)-1]] = value
	return branch
}

func collectKeys(prefix string, data map[string]any, visiting map[uintptr]bool, result *[]string) {
	identity := reflect.ValueOf(data).Pointer()
	if visiting[identity] {
		return
	}
	visiting[identity] = true
	defer delete(visiting, identity)

	for key, value := range data {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if nested, ok := value.(map[string]any); ok {
			collectKeys(path, nested, visiting, result)
			continue
		}
		*result = append(*result, path)
	}
}
