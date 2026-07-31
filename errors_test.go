// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigError_Unwrap(t *testing.T) {
	cause := errors.New("file broken")
	err := NewLoadError("config.yaml", cause)
	assert.True(t, errors.Is(err, ErrConfigLoad))
	assert.True(t, errors.Is(err, cause))

	var ce *ConfigError
	assert.True(t, errors.As(err, &ce))
	assert.Equal(t, "Load", ce.Op)
	assert.Equal(t, "config.yaml", ce.Source)
	assert.Equal(t, ConfigErrorCodeLoad, ce.Code)
	assert.Equal(t, cause, ce.Err)
}

func TestConfigErrorCode_MatchesSentinelCategories(t *testing.T) {
	tests := []struct {
		code     ConfigErrorCode
		sentinel error
	}{
		{ConfigErrorCodeLoad, ErrConfigLoad},
		{ConfigErrorCodeFormat, ErrConfigFormat},
		{ConfigErrorCodeValidation, ErrConfigValidation},
		{ConfigErrorCodeNotFound, ErrConfigNotFound},
		{ConfigErrorCodeMerge, ErrConfigMerge},
		{ConfigErrorCodeFrozen, ErrConfigFrozen},
		{ConfigErrorCodeClosed, ErrConfigClosed},
		{ConfigErrorCodeAccess, ErrConfigAccess},
		{ConfigErrorCodeInvalid, ErrConfigInvalid},
	}
	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			err := &ConfigError{Op: "Test", Code: test.code}
			assert.ErrorIs(t, err, test.sentinel)
			assert.Contains(t, err.Error(), test.sentinel.Error())
		})
	}

	unknown := &ConfigError{Op: "Test", Code: ConfigErrorCode("unknown")}
	assert.False(t, unknown.Is(ErrConfigLoad))
	assert.Equal(t, "Test: unknown", unknown.Error())
}

func TestConfigError_OneCauseChain(t *testing.T) {
	cause := errors.New("provider unavailable")
	err := &ConfigError{Op: "Load", Code: ConfigErrorCodeLoad, Err: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.ErrorIs(t, err, cause)
	assert.Contains(t, err.Error(), "config load error: provider unavailable")

	var nilConfigError *ConfigError
	assert.Nil(t, nilConfigError.Unwrap())
	assert.False(t, nilConfigError.Is(ErrConfigLoad))
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

func TestConfigError_DeterministicContextOrder(t *testing.T) {
	ctx := map[string]any{}
	for c := 'a'; c <= 'z'; c++ {
		ctx[string(c)] = string(c) + "-value"
	}
	ce := &ConfigError{
		Op:      "Get",
		Key:     "some.key",
		Code:    ConfigErrorCodeAccess,
		Context: ctx,
	}

	first := ce.Error()
	for i := 0; i < 100; i++ {
		assert.Equal(t, first, ce.Error(), "iteration %d differs", i)
	}

	idxA := strings.Index(first, "a=a-value")
	idxZ := strings.Index(first, "z=z-value")
	assert.True(t, idxA >= 0 && idxZ > idxA, "expected sorted context keys; got %q", first)
}

func TestConfigError_LargeContextSummarized(t *testing.T) {
	big := make(map[string]any, 1000)
	for i := 0; i < 1000; i++ {
		big[fmt.Sprintf("k%04d", i)] = i
	}
	ce := &ConfigError{
		Op:   "Load",
		Code: ConfigErrorCodeLoad,
		Context: map[string]any{
			"foo": big,
		},
	}

	msg := ce.Error()
	assert.Contains(t, msg, "<map: 1000 entries>")

	assert.NotContains(t, msg, "k0500")
	assert.NotContains(t, msg, "k0999")
}

func TestConfigKeyNotFound_TruncatesAvailableKeys(t *testing.T) {
	keys := make([]string, 50)
	for i := 0; i < 50; i++ {
		keys[i] = fmt.Sprintf("key%02d", i)
	}
	err := NewNotFoundError("missing", keys)
	msg := err.Error()

	for i := 0; i < availableKeysCap; i++ {
		assert.Contains(t, msg, fmt.Sprintf("key%02d", i))
	}

	for i := availableKeysCap; i < 50; i++ {
		assert.NotContains(t, msg, fmt.Sprintf("key%02d", i))
	}
	expectedRemaining := 50 - availableKeysCap
	assert.Contains(t, msg, fmt.Sprintf("(%d more...)", expectedRemaining))

	var ce *ConfigError
	assert.True(t, errors.As(err, &ce))
	full, ok := ce.Context["available_keys"].([]string)
	assert.True(t, ok)
	assert.Len(t, full, 50)
}

func TestConfigKeyNotFound_SmallListNotTruncated(t *testing.T) {
	keys := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	err := NewNotFoundError("missing", keys)
	msg := err.Error()

	for _, k := range keys {
		assert.Contains(t, msg, k)
	}
	assert.NotContains(t, msg, "more...")
}

func TestConfigError_RendersCategory(t *testing.T) {
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
				"expected %q to contain category text %q", tc.err.Error(), tc.prefix)
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
