// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package dotenvparse

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSupportsGodotenvGrammarAndNestedKeys(t *testing.T) {
	result, err := Parse([]byte("BASE=service\nNAME=${BASE}-api\nserver.port=8080\nMULTI=\"first\nsecond\"\n"), nil)
	require.NoError(t, err)
	assert.Equal(t, "service-api", result["NAME"])
	assert.Equal(t, "first\nsecond", result["MULTI"])
	assert.Equal(t, 8080, result["server"].(map[string]any)["port"])
}

func TestParseCanSkipMalformedRecord(t *testing.T) {
	var issues []Issue
	result, err := Parse([]byte("GOOD=1\nnot valid\nAFTER=${GOOD}\n"), func(issue Issue) error {
		issues = append(issues, issue)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, 2, issues[0].Line)
	assert.Equal(t, 1, result["GOOD"])
	assert.Equal(t, 1, result["AFTER"])
}

func TestParseCanRejectMalformedRecord(t *testing.T) {
	want := errors.New("stop")
	_, err := Parse([]byte("GOOD=1\nnot valid\n"), func(Issue) error { return want })
	assert.ErrorIs(t, err, want)
}
