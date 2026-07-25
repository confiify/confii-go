// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

// This file fuzzes the file-format loaders (YAML, JSON, TOML).
//
// Beyond the implicit "does not panic" check that Go's fuzz harness
// provides for free, each Fuzz target asserts the following semantic
// invariants on the loader's return value:
//
//  1. Result/error exclusivity: Load must return either a non-nil result
//     OR a non-nil error, never both. (A nil/nil pair is permitted because
//     an empty file legitimately yields no configuration.)
//  2. Typed errors: every non-nil error must be a *confii.ConfigError so
//     callers can use errors.As / errors.Is reliably, and the underlying
//     sentinel must be confii.ErrConfigLoad or confii.ErrConfigFormat.
//  3. Self-merge safety: a successful parse must produce a value graph
//     that DeepMerge(result, result) can recurse through without
//     panicking. This catches any future regression where a parser
//     yields cyclic or otherwise pathological structures.
//  4. Self-merge idempotence: DeepMerge(result, result) must be value-equal
//     to result, treating two IEEE NaN values of the same type as equivalent.
//     This is a weaker form of full Encode/Parse round-trip but is uniform
//     across YAML/JSON/TOML since all three decode to a map[string]any (with
//     the documented caveat that nested values may have varying concrete
//     types per format).
//
// Empty-string keys are intentionally NOT rejected here: JSON allows
// "" as a key and YAML can produce an empty string key from a bare
// `:` document; both are legal Go data, just pathological for
// downstream flatten-by-dot consumers.

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/internal/dictutil"
)

// assertLoaderInvariants validates the four invariants documented above
// on a single (result, err) pair. It is shared across the YAML/JSON/TOML
// fuzz targets because they all return map[string]any.
func assertLoaderInvariants(t *testing.T, result map[string]any, err error) {
	t.Helper()

	if err != nil {
		// Invariant 1: result must be nil when err is non-nil.
		if result != nil {
			t.Fatalf("loader returned non-nil result with non-nil error: result=%v err=%v", result, err)
		}
		// Invariant 2: errors must be typed as *confii.ConfigError and
		// wrap one of the documented sentinel errors.
		var ce *confii.ConfigError
		if !errors.As(err, &ce) {
			t.Fatalf("loader error is not *confii.ConfigError: %T (%v)", err, err)
		}
		if !errors.Is(err, confii.ErrConfigLoad) && !errors.Is(err, confii.ErrConfigFormat) {
			t.Fatalf("loader error wraps neither ErrConfigLoad nor ErrConfigFormat: %v", err)
		}
		return
	}

	if result == nil {
		// Empty input legitimately yields no configuration.
		return
	}

	// Invariant 3: the result must safely deep-merge with itself
	// without panicking. A self-merge exercises any cyclic-graph
	// landmines because DeepMerge recurses through nested
	// map[string]any. (We do NOT require non-empty keys: JSON allows
	// "" as a key and YAML can produce an empty string key from a bare
	// `:` document; both are legal inputs whose loader output is
	// well-formed Go data even if pathological for downstream
	// flatten-by-dot consumers.)
	merged := dictutil.DeepMerge(result, result)

	// Invariant 4: idempotence under self-merge. DeepMerge(a, a) should
	// be value-equal to a (modulo Go map iteration order, which
	// reflect.DeepEqual handles correctly, except for IEEE NaN values.
	// TOML permits NaN, so compare those as equivalent configuration values.
	if !loaderValuesEqual(merged, result) {
		t.Fatalf("DeepMerge(result, result) != result: result=%v merged=%v", result, merged)
	}
}

// loaderValuesEqual applies reflect.DeepEqual semantics while treating two
// IEEE NaN values of the same type as equivalent. TOML explicitly permits NaN,
// whose language-level inequality to itself does not violate merge idempotence.
func loaderValuesEqual(left, right any) bool {
	if reflect.DeepEqual(left, right) {
		return true
	}
	return loaderReflectEqual(reflect.ValueOf(left), reflect.ValueOf(right))
}

func loaderReflectEqual(left, right reflect.Value) bool {
	if !left.IsValid() || !right.IsValid() {
		return left.IsValid() == right.IsValid()
	}
	if left.Type() != right.Type() {
		return false
	}

	switch left.Kind() {
	case reflect.Interface:
		if left.IsNil() || right.IsNil() {
			return left.IsNil() == right.IsNil()
		}
		return loaderReflectEqual(left.Elem(), right.Elem())
	case reflect.Map:
		if left.IsNil() != right.IsNil() || left.Len() != right.Len() {
			return false
		}
		for _, key := range left.MapKeys() {
			rightValue := right.MapIndex(key)
			if !rightValue.IsValid() || !loaderReflectEqual(left.MapIndex(key), rightValue) {
				return false
			}
		}
		return true
	case reflect.Array, reflect.Slice:
		if left.Kind() == reflect.Slice && left.IsNil() != right.IsNil() {
			return false
		}
		if left.Len() != right.Len() {
			return false
		}
		for index := range left.Len() {
			if !loaderReflectEqual(left.Index(index), right.Index(index)) {
				return false
			}
		}
		return true
	case reflect.Float32, reflect.Float64:
		return math.IsNaN(left.Float()) && math.IsNaN(right.Float())
	default:
		return reflect.DeepEqual(left.Interface(), right.Interface())
	}
}

func TestLoaderValuesEqualTreatsNaNsAsEquivalent(t *testing.T) {
	left := map[string]any{
		"nested": []any{math.NaN(), float32(math.NaN())},
	}
	right := map[string]any{
		"nested": []any{math.NaN(), float32(math.NaN())},
	}

	if !loaderValuesEqual(left, right) {
		t.Fatal("equivalent nested NaN values should compare equal")
	}
	if loaderValuesEqual(left, map[string]any{"nested": []any{1.0, float32(1)}}) {
		t.Fatal("NaN values should not compare equal to finite values")
	}
}

func FuzzYAMLLoader(f *testing.F) {
	seeds := []string{
		"key: value",
		"a:\n  b: 1\n  c: true",
		"list:\n  - one\n  - two",
		"---\nkey: value",
		"",
		": invalid",
		"key: [1, 2, 3]",
		"key: {nested: true}",
		"key: null",
		"unicode: こんにちは",
		"multiline: |\n  line1\n  line2",
		"anchor: &a\n  k: v\nref: *a",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
		loader := NewYAML(path)
		result, err := loader.Load(context.Background())
		assertLoaderInvariants(t, result, err)
	})
}

func FuzzJSONLoader(f *testing.F) {
	seeds := []string{
		`{"key": "value"}`,
		`{"a": {"b": 1, "c": true}}`,
		`{"list": [1, 2, 3]}`,
		"",
		"not json",
		`{"key": null}`,
		`{"unicode": "こんにちは"}`,
		`{"nested": {"deep": {"value": 42}}}`,
		"{",
		`{"key": "value",}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
		loader := NewJSON(path)
		result, err := loader.Load(context.Background())
		assertLoaderInvariants(t, result, err)
	})
}

func FuzzTOMLLoader(f *testing.F) {
	seeds := []string{
		"key = \"value\"",
		"[section]\nkey = \"value\"",
		"list = [1, 2, 3]",
		"",
		"invalid toml {{",
		"num = 42",
		"bool = true",
		"float = 3.14",
		"0 = nan",
		"[a.b]\nc = \"deep\"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}
		loader := NewTOML(path)
		result, err := loader.Load(context.Background())
		assertLoaderInvariants(t, result, err)
	})
}
