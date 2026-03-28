package typecoerce

import "testing"

func FuzzParseScalar(f *testing.F) {
	// Seed corpus with representative values.
	seeds := []string{
		"true", "false", "yes", "no", "on", "off",
		"0", "1", "-1", "42", "9999999999999999999",
		"3.14", "1e10", "1.5e-3", ".5", "1.", "1.0e999",
		"", " ", "hello", "hello world",
		"007", "0x1F", "0b1010", "0o77",
		"NaN", "Inf", "-Inf", "+Inf",
		"TRUE", "False", "YES", "NO",
		"null", "nil", "None",
		"1,000", "1_000", "1 000",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic for any input.
		result := ParseScalar(input, false)
		if result == nil {
			t.Fatal("ParseScalar returned nil")
		}

		resultExt := ParseScalar(input, true)
		if resultExt == nil {
			t.Fatal("ParseScalar (extended) returned nil")
		}
	})
}
