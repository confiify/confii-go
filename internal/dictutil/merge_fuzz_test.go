package dictutil

import "testing"

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
		// Must not panic for any key path.
		_, _ = GetNested(data, keyPath)
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
		// Must not panic for any key path.
		_ = SetNested(data, keyPath, "value")
	})
}
