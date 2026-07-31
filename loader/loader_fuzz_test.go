// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/dictutil"
)

func assertLoaderInvariants(t *testing.T, result map[string]any, err error) {
	t.Helper()

	if err != nil {

		if result != nil {
			t.Fatalf("loader returned non-nil result with non-nil error: result=%v err=%v", result, err)
		}

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

		return
	}

	merged := dictutil.DeepMerge(result, result)

	if !loaderValuesEqual(merged, result) {
		t.Fatalf("DeepMerge(result, result) != result: result=%v merged=%v", result, merged)
	}
}

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
