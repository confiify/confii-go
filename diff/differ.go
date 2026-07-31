// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package diff provides configuration comparison and drift detection.
package diff

import (
	"encoding/json"
	"sort"
	"strings"
)

// DiffType categorizes a configuration difference.
type DiffType string

const (
	// Added identifies a key present only in the second configuration.
	Added DiffType = "added"
	// Removed identifies a key present only in the first configuration.
	Removed DiffType = "removed"
	// Modified identifies a key present in both configurations with unequal values.
	Modified DiffType = "modified"
)

// ConfigDiff describes one difference between two configurations. Key is the
// key at the current level, while Path is its complete dot-separated path.
// Modified map values include recursively populated NestedDiffs.
type ConfigDiff struct {
	// Key is the key name relative to the containing map.
	Key string `json:"key"`
	// Type classifies the difference as Added, Removed, or Modified.
	Type DiffType `json:"type"`
	// OldValue is populated for Removed and Modified entries.
	OldValue any `json:"old_value,omitempty"`
	// NewValue is populated for Added and Modified entries.
	NewValue any `json:"new_value,omitempty"`
	// Path is the complete dot-separated path from the comparison root.
	Path string `json:"path"`
	// NestedDiffs contains child differences when both values are maps.
	NestedDiffs []ConfigDiff `json:"nested_diffs,omitempty"`
}

// Redact returns a copy of diffs with values at, below, or above any sensitive
// path replaced by replacement. Parent entries are protected because their map
// values may contain a sensitive descendant. The input slice is not mutated.
func Redact(diffs []ConfigDiff, sensitivePaths []string, replacement any) []ConfigDiff {
	result := make([]ConfigDiff, len(diffs))
	for index, item := range diffs {
		result[index] = item
		if matchesSensitivePath(item.Path, sensitivePaths) {
			if item.OldValue != nil {
				result[index].OldValue = replacement
			}
			if item.NewValue != nil {
				result[index].NewValue = replacement
			}
		}
		result[index].NestedDiffs = Redact(item.NestedDiffs, sensitivePaths, replacement)
	}
	return result
}

func matchesSensitivePath(path string, sensitivePaths []string) bool {
	for _, sensitive := range sensitivePaths {
		if path == sensitive || strings.HasPrefix(path, sensitive+".") || strings.HasPrefix(sensitive, path+".") {
			return true
		}
	}
	return false
}

// Diff compares config1 with config2. Results are ordered lexicographically by
// key at every map level. Values are compared by their JSON representation;
// types that JSON cannot distinguish may therefore compare equal. Inputs are
// not mutated, but values in the returned entries may refer to input maps or
// slices and should be treated as read-only.
func Diff(config1, config2 map[string]any) []ConfigDiff {
	return diffMaps(config1, config2, "")
}

func diffMaps(c1, c2 map[string]any, prefix string) []ConfigDiff {
	var diffs []ConfigDiff

	// Collect all keys from both configs.
	keys := make(map[string]struct{})
	for k := range c1 {
		keys[k] = struct{}{}
	}
	for k := range c2 {
		keys[k] = struct{}{}
	}

	sortedKeys := make([]string, 0, len(keys))
	for k := range keys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, k := range sortedKeys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		v1, in1 := c1[k]
		v2, in2 := c2[k]

		switch {
		case !in1:
			diffs = append(diffs, ConfigDiff{Key: k, Type: Added, NewValue: v2, Path: path})
		case !in2:
			diffs = append(diffs, ConfigDiff{Key: k, Type: Removed, OldValue: v1, Path: path})
		default:
			m1, m1Ok := v1.(map[string]any)
			m2, m2Ok := v2.(map[string]any)
			if m1Ok && m2Ok {
				nested := diffMaps(m1, m2, path)
				if len(nested) > 0 {
					diffs = append(diffs, ConfigDiff{
						Key: k, Type: Modified, OldValue: v1, NewValue: v2,
						Path: path, NestedDiffs: nested,
					})
				}
			} else if !equal(v1, v2) {
				diffs = append(diffs, ConfigDiff{
					Key: k, Type: Modified, OldValue: v1, NewValue: v2, Path: path,
				})
			}
		}
	}

	return diffs
}

func equal(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

// Summary recursively counts diffs and returns keys "total", "added",
// "removed", and "modified". Parent Modified entries and their nested
// differences are each included in total and modified counts.
func Summary(diffs []ConfigDiff) map[string]int {
	s := map[string]int{"total": 0, "added": 0, "removed": 0, "modified": 0}
	countDiffs(diffs, s)
	return s
}

func countDiffs(diffs []ConfigDiff, s map[string]int) {
	for _, d := range diffs {
		s["total"]++
		s[string(d.Type)]++
		if len(d.NestedDiffs) > 0 {
			countDiffs(d.NestedDiffs, s)
		}
	}
}

// ToJSON serializes diffs as indented JSON. It returns an error when a value in
// OldValue or NewValue cannot be represented by encoding/json.
func ToJSON(diffs []ConfigDiff) (string, error) {
	data, err := json.MarshalIndent(diffs, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
