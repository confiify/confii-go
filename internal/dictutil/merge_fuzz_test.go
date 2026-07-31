// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package dictutil

import (
	"reflect"
	"strings"
	"testing"
)

func FuzzGetNested(f *testing.F) {
	seeds := []string{
		"a", "a.b", "a.b.c", "a.b.c.d.e.f",
		"", ".", "..", "...",
		"a.", ".a", ".a.b.",
		"key with spaces",
		"key.with.many.nested.levels.deep",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	data := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "value",
				"d": 42,
			},
			"e": "flat",
		},
		"top": "level",
	}

	f.Fuzz(func(t *testing.T, keyPath string) {

		snapshot := deepCopyMap(data)

		val, ok := GetNested(data, keyPath)

		if !reflect.DeepEqual(data, snapshot) {
			t.Fatalf("GetNested mutated input: before=%v after=%v", snapshot, data)
		}

		val2, ok2 := GetNested(data, keyPath)
		if ok != ok2 {
			t.Fatalf("GetNested returned different ok on repeat call: %v vs %v (path=%q)", ok, ok2, keyPath)
		}
		if !reflect.DeepEqual(val, val2) {
			t.Fatalf("GetNested returned different value on repeat call: %v vs %v (path=%q)", val, val2, keyPath)
		}
	})
}

func FuzzSetNested(f *testing.F) {
	seeds := []string{
		"a", "a.b", "a.b.c", "a.b.c.d.e.f",
		"", ".", "..", "...",
		"a.", ".a", ".a.b.",
		"key with spaces",
		"very.deep.nested.path.that.goes.on",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, keyPath string) {
		data := make(map[string]any)
		const sentinel = "fuzz-sentinel-value"

		err := SetNested(data, keyPath, sentinel)
		if err != nil {

			var pe *PathError
			if !asPathError(err, &pe) {
				t.Fatalf("SetNested returned non-PathError on empty map: %T (%v) path=%q", err, err, keyPath)
			}
			return
		}

		got, ok := GetNested(data, keyPath)
		if !ok {
			t.Fatalf("GetNested could not find value just set by SetNested: path=%q data=%v", keyPath, data)
		}
		if got != sentinel {
			t.Fatalf("GetNested returned wrong value after SetNested: want=%q got=%v path=%q", sentinel, got, keyPath)
		}
	})
}

func FuzzDeepMerge(f *testing.F) {
	type seed struct{ a, b string }
	seeds := []seed{
		{"a.b.c", "a.b.d"},
		{"x", "y"},
		{"a", "a"},
		{"", ""},
		{"a.b", "a"},
		{"a", "a.b"},
		{"deep.path.one", "deep.path.two"},
		{".", "."},
		{"a.b.c.d.e", "a.b.c.d.f"},
	}
	for _, s := range seeds {
		f.Add(s.a, s.b)
	}

	f.Fuzz(func(t *testing.T, pathA, pathB string) {

		if isComponentPrefix(pathA, pathB) || isComponentPrefix(pathB, pathA) {
			return
		}

		mapA := map[string]any{}
		if err := SetNested(mapA, pathA, "value-a"); err != nil {
			return
		}
		mapB := map[string]any{}
		if err := SetNested(mapB, pathB, "value-b"); err != nil {
			return
		}

		empty := map[string]any{}
		snapshotA := deepCopyMap(mapA)
		snapshotB := deepCopyMap(mapB)

		if got := DeepMerge(mapA, empty); !reflect.DeepEqual(got, mapA) {
			t.Fatalf("DeepMerge(a, {}) != a: a=%v got=%v", mapA, got)
		}

		if got := DeepMerge(empty, mapB); !reflect.DeepEqual(got, mapB) {
			t.Fatalf("DeepMerge({}, b) != b: b=%v got=%v", mapB, got)
		}

		if got := DeepMerge(mapA, mapA); !reflect.DeepEqual(got, mapA) {
			t.Fatalf("DeepMerge(a, a) != a: a=%v got=%v", mapA, got)
		}

		_ = DeepMerge(mapA, mapB)
		if !reflect.DeepEqual(mapA, snapshotA) {
			t.Fatalf("DeepMerge mutated base: before=%v after=%v", snapshotA, mapA)
		}
		if !reflect.DeepEqual(mapB, snapshotB) {
			t.Fatalf("DeepMerge mutated overlay: before=%v after=%v", snapshotB, mapB)
		}
	})
}

func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if nested, ok := v.(map[string]any); ok {
			out[k] = deepCopyMap(nested)
		} else {
			out[k] = v
		}
	}
	return out
}

func asPathError(err error, target **PathError) bool {
	if pe, ok := err.(*PathError); ok {
		*target = pe
		return true
	}
	return false
}

func isComponentPrefix(a, b string) bool {
	if a == b {
		return false
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	if len(aParts) >= len(bParts) {
		return false
	}
	for i, p := range aParts {
		if bParts[i] != p {
			return false
		}
	}
	return true
}
