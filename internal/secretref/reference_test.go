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
		"key only":      {Reference{Key: "k"}, "${secret:k}"},
		"field":         {Reference{Key: "k", Field: "f"}, "${secret:k:f}"},
		"field version": {Reference{Key: "k", Field: "f", Version: "v"}, "${secret:k:f:v}"},
		"version only":  {Reference{Key: "k", Version: "v"}, "${secret:k::v}"},
		"provider":      {Reference{Provider: "p", Key: "k"}, "${secret@p:k}"},
		"provider full": {Reference{Provider: "p", Key: "k", Field: "f", Version: "v"}, "${secret@p:k:f:v}"},
		// A key is the one component every reference must have, so this value
		// has no canonical form; see
		// TestString_UnrepresentableNeverSerializesToAReference.
		"no key is a diagnostic": {
			Reference{Field: "f", Version: "v"},
			"%!secret(key must not be empty)",
		},
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

func TestValidate_AcceptsWhatTheGrammarCanWrite(t *testing.T) {
	for _, ref := range []Reference{
		{Key: "k"},
		{Key: "db/creds", Field: "password"},
		{Key: "k", Field: "f", Version: "v"},
		{Key: "k", Version: "v"},
		{Provider: "vault", Key: "k"},
		{Provider: "aws-prod", Key: "k", Field: "f", Version: "v"},
		{Provider: "p.q_r-1", Key: "k"},
		// '{' and '$' are ordinary characters inside a component. The grammar
		// deliberately admits them; only ':' and '}' delimit.
		{Key: "a${b", Field: "c{d"},
	} {
		t.Run(ref.Key+"|"+ref.Field, func(t *testing.T) {
			assert.NoError(t, ref.Validate())
		})
	}
}

func TestValidate_RejectsWhatTheGrammarCannotWrite(t *testing.T) {
	for name, tc := range map[string]struct {
		ref  Reference
		want string
	}{
		"no key":                {Reference{}, "key"},
		"key holds a separator": {Reference{Key: "key:segment"}, "key"},
		"key holds a closer":    {Reference{Key: "key}tail"}, "key"},
		"field holds a separator": {
			Reference{Key: "k", Field: "a:b"}, "field"},
		"version holds a closer": {
			Reference{Key: "k", Version: "1}"}, "version"},
		"provider holds a space": {
			Reference{Provider: "bad alias", Key: "k"}, "provider"},
		"provider starts with a hyphen": {
			Reference{Provider: "-p", Key: "k"}, "provider"},
		"provider holds a separator": {
			Reference{Provider: "p:q", Key: "k"}, "provider"},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.ref.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrUnrepresentable)
			assert.Contains(t, err.Error(), tc.want,
				"the error must name the component at fault")
		})
	}
}

// A Reference is a struct with exported fields, so a caller can build one the
// grammar cannot express. String must not answer such a value with text that
// reads as a reference: ${secret:key:segment} would resolve, and it would
// resolve to a different secret than the one the fields named.
func TestString_UnrepresentableNeverSerializesToAReference(t *testing.T) {
	for name, ref := range map[string]Reference{
		"zero value":             {},
		"key holds a separator":  {Key: "key:segment"},
		"key holds a closer":     {Key: "key}tail"},
		"field holds a closer":   {Key: "k", Field: "f}"},
		"version separator":      {Key: "k", Version: "1:2"},
		"provider holds a space": {Provider: "bad alias", Key: "k"},
	} {
		t.Run(name, func(t *testing.T) {
			out := ref.String()
			assert.False(t, Contains(out),
				"an unrepresentable reference serialized to resolvable text: %q", out)
			_, err := Parse(out)
			assert.Error(t, err)
			assert.Contains(t, out, "%!secret(",
				"the result must be visibly not a reference, got %q", out)
		})
	}
}

func TestMarshalText_ReportsWhatStringCannotSay(t *testing.T) {
	valid := Reference{Provider: "p", Key: "k", Version: "v"}
	text, err := valid.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "${secret@p:k::v}", string(text))

	_, err = Reference{Key: "key:segment"}.MarshalText()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnrepresentable)
}

func TestUnmarshalText_RoundTripsThroughEncoding(t *testing.T) {
	want := Reference{Provider: "vault", Key: "db/creds", Field: "password", Version: "3"}
	text, err := want.MarshalText()
	require.NoError(t, err)

	var got Reference
	require.NoError(t, got.UnmarshalText(text))
	assert.Equal(t, want, got)

	require.Error(t, new(Reference).UnmarshalText([]byte("not a reference")))
}

// FuzzSerialization states the whole contract as one property, over components
// the fuzzer is free to make hostile: a Reference either serializes to
// something that parses back to itself, or serializes to something that is not
// a reference at all. There is no third outcome, and in particular no outcome
// where String yields a resolvable reference naming a different secret.
func FuzzSerialization(f *testing.F) {
	f.Add("", "k", "", "")
	f.Add("p", "db/creds", "password", "3")
	f.Add("", "key:segment", "", "")
	f.Add("bad alias", "k", "", "")
	f.Add("", "k}", "f", "v")
	f.Add("", "a${b", "c{d", "")

	f.Fuzz(func(t *testing.T, provider, key, field, version string) {
		ref := Reference{Provider: provider, Key: key, Field: field, Version: version}
		out := ref.String()
		if err := ref.Validate(); err != nil {
			if Contains(out) {
				t.Fatalf("unrepresentable %#v serialized to resolvable text %q", ref, out)
			}
			return
		}
		again, err := Parse(out)
		if err != nil {
			t.Fatalf("valid %#v serialized to %q, which does not parse: %v", ref, out, err)
		}
		if again != ref {
			t.Fatalf("%#v serialized to %q, which parsed back as %#v", ref, out, again)
		}
	})
}
