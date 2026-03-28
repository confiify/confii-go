package hook

import "testing"

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
		// Must not panic for any input.
		_ = hook("testkey", input)
	})
}
