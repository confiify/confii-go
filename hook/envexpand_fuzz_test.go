// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"
	"testing"
)

func FuzzEnvExpanderHook(f *testing.F) {
	seeds := []string{
		"no vars",
		"${HOME}",
		"${PATH}",
		"prefix_${HOME}_suffix",
		"${A}${B}${C}",
		"${NONEXISTENT}",
		"${}", "${", "}",
		"$${escaped}",
		"${:invalid}",
		"${key:default}",
		"",
		"${A_B_C}",
		"${123}",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	hook := NewEnvExpanderHook()

	f.Fuzz(func(t *testing.T, input string) {

		_, _ = hook(context.Background(), "testkey", input)
	})
}
