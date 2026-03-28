package loader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
		_, _ = loader.Load(context.Background())
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
		_, _ = loader.Load(context.Background())
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
		_, _ = loader.Load(context.Background())
	})
}
