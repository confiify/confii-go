// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secretref

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_EveryForm(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Reference
	}{
		{"${secret:key}", Reference{Key: "key"}},
		{"${secret:db/creds:password}", Reference{Key: "db/creds", Field: "password"}},
		{"${secret:db/creds:password:3}", Reference{Key: "db/creds", Field: "password", Version: "3"}},
		{"${secret:db/creds::3}", Reference{Key: "db/creds", Version: "3"}},
		{"${secret@vault:key}", Reference{Provider: "vault", Key: "key"}},
		{"${secret@aws-prod:k:f:v}", Reference{Provider: "aws-prod", Key: "k", Field: "f", Version: "v"}},
		{"${secret@p.q_r-1:k}", Reference{Provider: "p.q_r-1", Key: "k"}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := Parse(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParse_Rejects(t *testing.T) {
	for name, input := range map[string]string{
		"empty":              "",
		"plain text":         "value",
		"no key":             "${secret:}",
		"unterminated":       "${secret:key",
		"wrong scheme":       "${notsecret:key}",
		"empty provider":     "${secret@:key}",
		"provider bad start": "${secret@-bad:key}",
		"leading text":       "x${secret:key}",
		"trailing text":      "${secret:key}x",
		"two references":     "${secret:a}${secret:b}",
		"too many parts":     "${secret:a:b:c:d}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(input)
			require.Error(t, err)
			var refErr *Error
			require.ErrorAs(t, err, &refErr)
			assert.NotEmpty(t, refErr.Reason)
		})
	}
}

func TestError_Message(t *testing.T) {
	err := &Error{Input: "${secret:k", Reason: "unterminated"}
	assert.Contains(t, err.Error(), "${secret:k")
	assert.Contains(t, err.Error(), "unterminated")
}

func TestString_Canonical(t *testing.T) {
	for name, tc := range map[string]struct {
		ref  Reference
		want string
	}{
		"key only":        {Reference{Key: "k"}, "${secret:k}"},
		"field":           {Reference{Key: "k", Field: "f"}, "${secret:k:f}"},
		"field version":   {Reference{Key: "k", Field: "f", Version: "v"}, "${secret:k:f:v}"},
		"version only":    {Reference{Key: "k", Version: "v"}, "${secret:k::v}"},
		"provider":        {Reference{Provider: "p", Key: "k"}, "${secret@p:k}"},
		"provider full":   {Reference{Provider: "p", Key: "k", Field: "f", Version: "v"}, "${secret@p:k:f:v}"},
		"no key is empty": {Reference{Field: "f", Version: "v"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.ref.String())
		})
	}
}

func TestString_RoundTrips(t *testing.T) {
	for _, ref := range []Reference{
		{Key: "k"},
		{Key: "k", Field: "f"},
		{Key: "k", Field: "f", Version: "v"},
		{Key: "k", Version: "v"},
		{Provider: "p", Key: "k"},
		{Provider: "p", Key: "k", Field: "f", Version: "v"},
	} {
		t.Run(ref.String(), func(t *testing.T) {
			again, err := Parse(ref.String())
			require.NoError(t, err)
			assert.Equal(t, ref, again)
		})
	}
}

func TestContains(t *testing.T) {
	assert.True(t, Contains("a ${secret:k} b"))
	assert.True(t, Contains("${secret@p:k}"))
	assert.False(t, Contains("no references"))
	assert.False(t, Contains("${env:HOME}"))
	// The literal prefix is present but the grammar is not satisfied.
	assert.False(t, Contains("${secret"))
	assert.False(t, Contains("${secret:}"))
}

func TestFind(t *testing.T) {
	refs := Find("u:${secret:a}@h/${secret@p:b:f}")
	require.Len(t, refs, 2)
	assert.Equal(t, Reference{Key: "a"}, refs[0])
	assert.Equal(t, Reference{Provider: "p", Key: "b", Field: "f"}, refs[1])
}

func TestFind_NoMatchesReturnsNil(t *testing.T) {
	assert.Nil(t, Find("nothing here"), "no matches must yield nil, not an empty slice")
}

func TestFromMatch_ShortSliceIsSafe(t *testing.T) {
	// FromMatch is fed regexp output, but a defensive shape check keeps a
	// malformed caller from panicking.
	assert.Equal(t, Reference{}, FromMatch(nil))
	assert.Equal(t, Reference{}, FromMatch([]string{"${secret:k}"}))
	assert.Equal(t, Reference{Provider: "p"}, FromMatch([]string{"m", "p"}))
}

func TestPattern_IsShared(t *testing.T) {
	require.NotNil(t, Pattern())
	assert.Same(t, Pattern(), Pattern(), "callers must share one compiled grammar")
}
