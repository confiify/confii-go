// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecodeWithOptionsStrictRejectsScalarConversion proves a caller
// can require exact input types: a quoted integer is a decode failure
// rather than a silent conversion.
func TestDecodeWithOptionsStrictRejectsScalarConversion(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{"host": "localhost", "port": "5432"},
	}

	strict, err := DecodeWithOptions[TestConfig](data, Options{WeaklyTypedInput: false})
	require.Error(t, err)
	assert.Nil(t, strict)
	assert.Contains(t, err.Error(), "struct decode")
}

// TestDecodeWithOptionsWeakKeepsHistoricalBehavior pins the default:
// weak conversion still accepts a quoted integer.
func TestDecodeWithOptionsWeakKeepsHistoricalBehavior(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{"host": "localhost", "port": "5432"},
	}

	weak, err := DecodeWithOptions[TestConfig](data, Options{WeaklyTypedInput: true})
	require.NoError(t, err)
	assert.Equal(t, 5432, weak.Database.Port)

	implicit, err := Decode[TestConfig](data)
	require.NoError(t, err)
	assert.Equal(t, 5432, implicit.Database.Port)
}

// TestDecodeAndValidateWithOptionsStrict proves the combined entry
// point honors the same policy and still runs `validate` tags.
func TestDecodeAndValidateWithOptionsStrict(t *testing.T) {
	quoted := map[string]any{
		"database": map[string]any{"host": "localhost", "port": "5432", "name": "app"},
	}
	_, err := DecodeAndValidateWithOptions[TestConfig](quoted, Options{WeaklyTypedInput: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "struct decode")

	exact := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432, "name": "app"},
	}
	model, err := DecodeAndValidateWithOptions[TestConfig](exact, Options{WeaklyTypedInput: false})
	require.NoError(t, err)
	assert.Equal(t, 5432, model.Database.Port)

	// Validation still applies under strict decoding.
	outOfRange := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 99999, "name": "app"},
	}
	_, err = DecodeAndValidateWithOptions[TestConfig](outOfRange, Options{WeaklyTypedInput: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "struct validation")
}

// TestNewStructValidatorWithOptionsStrict proves the reusable
// validator carries the policy too.
func TestNewStructValidatorWithOptionsStrict(t *testing.T) {
	quoted := map[string]any{
		"database": map[string]any{"host": "localhost", "port": "5432", "name": "app"},
	}

	strict := NewStructValidatorWithOptions[TestConfig](Options{WeaklyTypedInput: false})
	require.Error(t, strict.Validate(quoted))

	weak := NewStructValidator[TestConfig]()
	require.NoError(t, weak.Validate(quoted))
}

// TestDecodeWithOptionsRejectUnknownKeys proves an undeclared key can
// be made an error instead of a silently unused input.
func TestDecodeWithOptionsRejectUnknownKeys(t *testing.T) {
	typo := map[string]any{
		"database": map[string]any{"host": "localhost", "prot": 5432, "name": "app"},
	}

	// Default: the typo is silently unused and Port stays zero.
	lenient, err := DecodeWithOptions[TestConfig](typo, Options{WeaklyTypedInput: true})
	require.NoError(t, err)
	assert.Zero(t, lenient.Database.Port)

	// Enabled: the decode fails and names the offending key.
	_, err = DecodeWithOptions[TestConfig](typo, Options{
		WeaklyTypedInput:  true,
		RejectUnknownKeys: true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prot")
}

// TestRejectUnknownKeysAcceptsDeclaredInput proves the option rejects
// only undeclared keys, not valid configuration.
func TestRejectUnknownKeysAcceptsDeclaredInput(t *testing.T) {
	clean := map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432, "name": "app"},
		"debug":    true,
	}
	model, err := DecodeAndValidateWithOptions[TestConfig](clean, Options{
		WeaklyTypedInput:  true,
		RejectUnknownKeys: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 5432, model.Database.Port)
	assert.True(t, model.Debug)
}
