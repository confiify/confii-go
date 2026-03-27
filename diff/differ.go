// Package diff provides configuration comparison and drift detection.
package diff

import (
	"encoding/json"
	"sort"
)

// DiffType categorizes a configuration difference.
type DiffType string

const (
	Added    DiffType = "added"
	Removed  DiffType = "removed"
	Modified DiffType = "modified"
)

// ConfigDiff represents a single difference between two configurations.
type ConfigDiff struct {
	Key         string       `json:"key"`
	Type        DiffType     `json:"type"`
	OldValue    any          `json:"old_value,omitempty"`
	NewValue    any          `json:"new_value,omitempty"`
	Path        string       `json:"path"`
	NestedDiffs []ConfigDiff `json:"nested_diffs,omitempty"`
}

// Diff compares two configuration maps and returns a list of differences.
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

// Summary returns a count summary of diffs.
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

// ToJSON serializes diffs to JSON.
func ToJSON(diffs []ConfigDiff) (string, error) {
	data, err := json.MarshalIndent(diffs, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
