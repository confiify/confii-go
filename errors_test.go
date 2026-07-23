package confii

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigError_Unwrap(t *testing.T) {
	err := NewLoadError("config.yaml", errors.New("file broken"))
	assert.True(t, errors.Is(err, ErrConfigLoad))

	var ce *ConfigError
	assert.True(t, errors.As(err, &ce))
	assert.Equal(t, "Load", ce.Op)
	assert.Equal(t, "config.yaml", ce.Source)
}

func TestConfigError_ErrorMessage(t *testing.T) {
	err := NewNotFoundError("database.host", []string{"database.port", "debug"})
	msg := err.Error()
	assert.Contains(t, msg, "database.host")
	assert.Contains(t, msg, "config key not found")
}

func TestNewFormatError(t *testing.T) {
	err := NewFormatError("bad.yaml", "yaml", errors.New("parse fail"))
	assert.True(t, errors.Is(err, ErrConfigFormat))
	assert.Contains(t, err.Error(), "yaml")
}

func TestNewFrozenError(t *testing.T) {
	err := NewFrozenError("Set")
	assert.True(t, errors.Is(err, ErrConfigFrozen))
	assert.Contains(t, err.Error(), "Set")
}

// TestConfigError_DeterministicContextOrder verifies that ConfigError.Error()
// renders Context entries in a stable, alphabetical order regardless of Go's
// randomized map iteration. We invoke Error() many times against the same
// struct and assert every rendering is byte-identical.
func TestConfigError_DeterministicContextOrder(t *testing.T) {
	ctx := map[string]any{}
	for c := 'a'; c <= 'z'; c++ {
		ctx[string(c)] = string(c) + "-value"
	}
	ce := &ConfigError{
		Op:      "Get",
		Key:     "some.key",
		Err:     ErrConfigAccess,
		Context: ctx,
	}

	first := ce.Error()
	for i := 0; i < 100; i++ {
		assert.Equal(t, first, ce.Error(), "iteration %d differs", i)
	}

	// Spot-check the alphabetical ordering: "a=" must appear before "b=" etc.
	idxA := strings.Index(first, "a=a-value")
	idxZ := strings.Index(first, "z=z-value")
	assert.True(t, idxA >= 0 && idxZ > idxA, "expected sorted context keys; got %q", first)
}

// TestConfigError_LargeContextSummarized verifies that nested map values in
// Context are not expanded inline; instead a bounded "<map: N entries>"
// summary is emitted so operator messages stay readable for big configs.
func TestConfigError_LargeContextSummarized(t *testing.T) {
	big := make(map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		big[fmt.Sprintf("k%04d", i)] = i
	}
	ce := &ConfigError{
		Op:  "Load",
		Err: ErrConfigLoad,
		Context: map[string]any{
			"foo": big,
		},
	}

	msg := ce.Error()
	assert.Contains(t, msg, "<map: 1000 entries>")
	// And critically: none of the actual keys should leak into the string.
	assert.NotContains(t, msg, "k0500")
	assert.NotContains(t, msg, "k0999")
}

// TestConfigKeyNotFound_TruncatesAvailableKeys verifies that a not-found
// error built against a 50-key config renders only the first availableKeysCap
// keys (alphabetically) followed by an "(N more...)" suffix, while leaving
// the full slice intact on Context for programmatic inspection.
func TestConfigKeyNotFound_TruncatesAvailableKeys(t *testing.T) {
	keys := make([]string, 50)
	for i := 0; i < 50; i++ {
		keys[i] = fmt.Sprintf("key%02d", i)
	}
	err := NewNotFoundError("missing", keys)
	msg := err.Error()

	// First 10 alphabetically are key00..key09.
	for i := 0; i < availableKeysCap; i++ {
		assert.Contains(t, msg, fmt.Sprintf("key%02d", i))
	}
	// Beyond the cap should NOT appear.
	for i := availableKeysCap; i < 50; i++ {
		assert.NotContains(t, msg, fmt.Sprintf("key%02d", i))
	}
	expectedRemaining := 50 - availableKeysCap
	assert.Contains(t, msg, fmt.Sprintf("(%d more...)", expectedRemaining))

	// Structured Context still carries every candidate so callers can
	// inspect the full list programmatically.
	var ce *ConfigError
	assert.True(t, errors.As(err, &ce))
	full, ok := ce.Context["available_keys"].([]string)
	assert.True(t, ok)
	assert.Len(t, full, 50)
}

// TestConfigKeyNotFound_SmallListNotTruncated verifies that lists at or below
// the cap are rendered in full with no "more..." suffix.
func TestConfigKeyNotFound_SmallListNotTruncated(t *testing.T) {
	keys := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	err := NewNotFoundError("missing", keys)
	msg := err.Error()

	for _, k := range keys {
		assert.Contains(t, msg, k)
	}
	assert.NotContains(t, msg, "more...")
}

// TestConfigError_PreservesPrefix guarantees the legacy sentinel-error
// prefixes survive the new rendering, so log pipelines and substring
// matchers built around strings like "config load error" / "config key not
// found" continue to function.
func TestConfigError_PreservesPrefix(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		prefix string
	}{
		{
			name:   "load error",
			err:    NewLoadError("config.yaml", errors.New("io fail")),
			prefix: "config load error",
		},
		{
			name:   "format error",
			err:    NewFormatError("bad.yaml", "yaml", errors.New("parse fail")),
			prefix: "config format error",
		},
		{
			name:   "not found error",
			err:    NewNotFoundError("missing.key", []string{"a", "b"}),
			prefix: "config key not found",
		},
		{
			name:   "validation error",
			err:    NewValidationError([]string{"bad"}, errors.New("oops")),
			prefix: "config validation error",
		},
		{
			name:   "frozen error",
			err:    NewFrozenError("Set"),
			prefix: "config is frozen",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, tc.err.Error(), tc.prefix,
				"expected %q to contain legacy prefix %q", tc.err.Error(), tc.prefix)
		})
	}
}

func TestFormatContextValue_AllBoundedShapes(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "<nil>"},
		{"string", "value", "value"},
		{"strings", []string{"b", "a"}, "[a, b]"},
		{"any slice", []any{1, "two"}, "<slice: 2 items>"},
		{"int slice", []int{1, 2, 3}, "<slice: 3 items>"},
		{"float slice", []float64{1, 2}, "<slice: 2 items>"},
		{"any map", map[string]any{"a": 1}, "<map: 1 entries>"},
		{"string map", map[string]string{"a": "b"}, "<map: 1 entries>"},
		{"int map", map[string]int{"a": 1}, "<map: 1 entries>"},
		{"scalar", 42, "42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatContextValue("key", tc.in))
		})
	}
}
