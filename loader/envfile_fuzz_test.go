// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

// This file fuzzes EnvFileLoader and unquoteEnvValue.
//
// FuzzEnvFileLoader runs the loader twice for each input — once under
// confii.ErrorPolicyIgnore, once under confii.ErrorPolicyRaise — and
// asserts the following semantic invariants on each call's return value:
//
//  1. Result/error exclusivity: Load must return either a non-nil result
//     OR a non-nil error, never both. (nil/nil is allowed for empty
//     input, see envfile.Load.)
//  2. Typed errors: under ErrorPolicyRaise, every non-nil error must be
//     a *confii.ConfigError wrapping confii.ErrConfigLoad. Under
//     ErrorPolicyIgnore, no error must be returned at all (since the
//     loader's only recoverable failure mode — malformed lines — is now
//     silenced).
//  3. Result well-formedness: every successful result must be
//     JSON-encodable. Empty top-level keys are tolerated because the
//     loader deliberately accepts "=value" / " = " lines under all
//     error policies — that's a documented quirk of the parser, not a
//     fuzz failure. JSON-encodability catches any future regression
//     where the parser produces channels, funcs, or other un-encodable
//     values (it also implicitly catches non-string-keyed nested maps,
//     which would break downstream serialization).
//
// FuzzUnquoteEnvValue retains its "must not panic" invariant and adds
// a length invariant: unquoteEnvValue never grows the input by more
// than the length of the longest escape expansion (\n -> 1 byte, \t ->
// 1 byte) — i.e. the result is bounded by len(input). This catches any
// future regression where unquoting introduces unbounded duplication.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go"
)

// jsonEncode is a thin wrapper to keep the invariant assertion readable.
func jsonEncode(v any) ([]byte, error) { return json.Marshal(v) }

// assertEnvFileInvariants validates the documented invariants for a
// single (result, err) pair returned by EnvFileLoader.Load. The policy
// argument tells the helper which error-policy contract to enforce.
func assertEnvFileInvariants(t *testing.T, policy confii.ErrorPolicy, result map[string]any, err error) {
	t.Helper()

	if err != nil {
		// Invariant 1: result must be nil when err is non-nil.
		if result != nil {
			t.Fatalf("envfile loader returned non-nil result with non-nil error: result=%v err=%v", result, err)
		}
		// Invariant 2: under Ignore, no error is permitted at all (the
		// only recoverable error path — malformed lines — is silenced
		// and os.IsNotExist is mapped to (nil, nil)).
		if policy == confii.ErrorPolicyIgnore {
			t.Fatalf("envfile loader under ErrorPolicyIgnore returned an error: %v", err)
		}
		// Invariant 2 (cont.): typed errors wrapping ErrConfigLoad.
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
		// Empty file or no parseable lines -> nil/nil is permitted.
		return
	}

	// Invariant 3 (relaxed): the loader must produce a valid
	// map[string]any whose value graph is finite and JSON-encodable.
	// Empty top-level keys are tolerated because the loader
	// deliberately accepts "=value" / " = " lines under all error
	// policies — that's a documented quirk of the parser and not a
	// fuzz failure. The semantic invariant we enforce is downstream
	// safety: the result must round-trip through encoding/json without
	// error, which catches any future regression where the parser
	// produces channels, funcs, or other un-encodable values.
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

		// Run under both error policies. Each policy enforces a
		// different contract; running both per input maximizes the
		// invariant coverage for a single fuzz iteration.
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
		// Invariant: the output is bounded by the input length. The
		// only transforms are (a) stripping outer quote pairs (which
		// only shrinks), (b) replacing two-byte escape sequences (\n,
		// \t) with one-byte runes (which also shrinks), and (c)
		// trimming trailing inline comments (shrinks). So len(out) must
		// never exceed len(input).
		if len(out) > len(input) {
			t.Fatalf("unquoteEnvValue grew the input: in=%q (%d bytes) out=%q (%d bytes)",
				input, len(input), out, len(out))
		}
	})
}
