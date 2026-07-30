// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func jsonEncode(v any) ([]byte, error) { return json.Marshal(v) }

func assertEnvFileInvariants(t *testing.T, policy confii.ErrorPolicy, result map[string]any, err error) {
	t.Helper()

	if err != nil {

		if result != nil {
			t.Fatalf("envfile loader returned non-nil result with non-nil error: result=%v err=%v", result, err)
		}

		if policy == confii.ErrorPolicyIgnore {
			t.Fatalf("envfile loader under ErrorPolicyIgnore returned an error: %v", err)
		}

		var ce *confii.ConfigError
		if !errors.As(err, &ce) {
			t.Fatalf("envfile loader error is not *confii.ConfigError: %T (%v)", err, err)
		}
		if !errors.Is(err, confii.ErrConfigLoad) {
			t.Fatalf("envfile loader error does not wrap ErrConfigLoad: %v", err)
		}
		return
	}

	if result == nil {

		return
	}

	if _, jerr := jsonEncode(result); jerr != nil {
		t.Fatalf("envfile result is not JSON-encodable: %v (result=%v)", jerr, result)
	}
}

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

		ignoreLoader := NewEnvFile(path, WithEnvFileErrorPolicy(confii.ErrorPolicyIgnore))
		gotResult, gotErr := ignoreLoader.Load(context.Background())
		assertEnvFileInvariants(t, confii.ErrorPolicyIgnore, gotResult, gotErr)

		raiseLoader := NewEnvFile(path, WithEnvFileErrorPolicy(confii.ErrorPolicyRaise))
		gotResult, gotErr = raiseLoader.Load(context.Background())
		assertEnvFileInvariants(t, confii.ErrorPolicyRaise, gotResult, gotErr)
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
		out := unquoteEnvValue(input)

		if len(out) > len(input) {
			t.Fatalf("unquoteEnvValue grew the input: in=%q (%d bytes) out=%q (%d bytes)",
				input, len(input), out, len(out))
		}
	})
}
