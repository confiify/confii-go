package loader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzEnvFileLoader(f *testing.F) {
	seeds := []string{
		"KEY=value",
		"KEY=value # comment",
		"KEY='single quoted'",
		`KEY="double quoted"`,
		`KEY="line\nbreak"`,
		`KEY="tab\there"`,
		"# comment line",
		"",
		"NOEQUALS",
		"EMPTY=",
		"SPACES = value with spaces ",
		"NESTED.KEY.PATH=deep",
		"MULTI=first\nSECOND=second",
		"KEY='unmatched",
		`KEY="unmatched`,
		"KEY=true",
		"KEY=42",
		"KEY=3.14",
		"KEY==equals=in=value",
		"=emptykey",
		" = ",
		"KEY='it\\'s quoted'",
		"KEY=\"escaped\\nvalue\"",
		"UNICODE=こんにちは",
		strings.Repeat("K=V\n", 1000),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".env")
		if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
			t.Fatal(err)
		}

		loader := NewEnvFile(path)
		// Must not panic for any input.
		_, _ = loader.Load(context.Background())
	})
}

func FuzzUnquoteEnvValue(f *testing.F) {
	seeds := []string{
		"plain", "'single'", `"double"`,
		`"with\nnewline"`, `"with\ttab"`,
		"value # comment", "value  #  comment",
		"'", `"`, "''", `""`,
		"'unmatched", `"unmatched`,
		"a'b", `a"b`,
		"", " ",
		`"nested 'quotes'"`,
		`'"nested "quotes"'`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic for any input.
		_ = unquoteEnvValue(input)
	})
}
