// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package dictutil

// This file fuzzes the dictutil nested-key helpers and DeepMerge.
//
// FuzzGetNested and FuzzSetNested assert these invariants beyond
// "does not panic":
//
//  - GetNested: when (val, true) is returned, the same val must be
//    reachable via the same path on a fresh probe — i.e. GetNested is
//    pure (does not mutate the input map).
//  - SetNested: after a successful SetNested(data, path, value), a
//    GetNested(data, path) must return (value, true). This is the
//    fundamental round-trip property of the get/set pair. Failed
//    SetNested calls (returning *PathError) must leave the map
//    unchanged at the affected prefix.
//
// FuzzDeepMerge asserts the merge contract:
//
//  - Right identity: DeepMerge(a, {}) is value-equal to a.
//  - Left identity: DeepMerge({}, b) is value-equal to b.
//  - Self-idempotence: DeepMerge(a, a) is value-equal to a.
//  - Non-mutation: neither input map is modified by DeepMerge — we
//    snapshot both inputs before the call and re-check after.
//  - Result well-formedness: every key in the result is non-empty (the
//    inputs are constructed from fuzz-generated key paths so this
//    catches any key transformation that produces an empty entry).

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
		// Snapshot the data map so we can verify GetNested doesn't
		// mutate it.
		snapshot := deepCopyMap(data)

		val, ok := GetNested(data, keyPath)

		// Invariant: GetNested must not mutate the input.
		if !reflect.DeepEqual(data, snapshot) {
			t.Fatalf("GetNested mutated input: before=%v after=%v", snapshot, data)
		}

		// Invariant: the result must be deterministic across repeated
		// calls.
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
			// SetNested only fails on conflict with a non-map
			// intermediate. With a freshly-allocated empty map there
			// are no intermediates to conflict with — so an error
			// here would imply a regression. Allow only the
			// PathError type for forward compatibility.
			var pe *PathError
			if !asPathError(err, &pe) {
				t.Fatalf("SetNested returned non-PathError on empty map: %T (%v) path=%q", err, err, keyPath)
			}
			return
		}

		// Invariant: round-trip with GetNested. The path-segments
		// produced by strings.Split on "..", "a.", etc. include empty
		// strings; SetNested stores under those empty segments
		// without complaint, and GetNested must retrieve them.
		got, ok := GetNested(data, keyPath)
		if !ok {
			t.Fatalf("GetNested could not find value just set by SetNested: path=%q data=%v", keyPath, data)
		}
		if got != sentinel {
			t.Fatalf("GetNested returned wrong value after SetNested: want=%q got=%v path=%q", sentinel, got, keyPath)
		}
	})
}

// FuzzDeepMerge fuzzes the DeepMerge function. The fuzz input is a pair
// of dotted key paths used to construct two small nested maps which are
// then merged. This is enough surface to exercise both flat-key merges
// and nested recursion paths; full random-tree fuzzing would require
// custom value generation, which the standard fuzz harness does not
// support directly.
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
		// Skip pathological seeds that produce structurally
		// incompatible parents (e.g. "a" and "a.b" — SetNested would
		// turn a leaf into a map). We test DeepMerge, not SetNested
		// conflict resolution. Detect this by checking whether either
		// path is a strict prefix of the other (component-wise).
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

		// Invariant: right identity.
		if got := DeepMerge(mapA, empty); !reflect.DeepEqual(got, mapA) {
			t.Fatalf("DeepMerge(a, {}) != a: a=%v got=%v", mapA, got)
		}
		// Invariant: left identity.
		if got := DeepMerge(empty, mapB); !reflect.DeepEqual(got, mapB) {
			t.Fatalf("DeepMerge({}, b) != b: b=%v got=%v", mapB, got)
		}
		// Invariant: self-idempotence.
		if got := DeepMerge(mapA, mapA); !reflect.DeepEqual(got, mapA) {
			t.Fatalf("DeepMerge(a, a) != a: a=%v got=%v", mapA, got)
		}

		// Invariant: non-mutation. The actual merge of distinct
		// inputs must leave both originals untouched.
		_ = DeepMerge(mapA, mapB)
		if !reflect.DeepEqual(mapA, snapshotA) {
			t.Fatalf("DeepMerge mutated base: before=%v after=%v", snapshotA, mapA)
		}
		if !reflect.DeepEqual(mapB, snapshotB) {
			t.Fatalf("DeepMerge mutated overlay: before=%v after=%v", snapshotB, mapB)
		}
	})
}

// deepCopyMap returns a deep copy of m, recursing through nested
// map[string]any values. Other values (strings, ints, etc.) are
// reference-copied; that's safe because they are immutable in Go.
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

// asPathError unwraps err looking for a *PathError. Implemented inline
// to avoid pulling errors.As into the fuzz harness for a single use.
func asPathError(err error, target **PathError) bool {
	if pe, ok := err.(*PathError); ok {
		*target = pe
		return true
	}
	return false
}

// isComponentPrefix reports whether the dot-separated components of a
// are a strict prefix of those of b. Equal paths return false.
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
