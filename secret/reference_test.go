// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret_test

import (
	"context"
	"testing"

	"github.com/confiify/confii-go/v2/secret"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReference_Forms(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  secret.Reference
	}{
		{"${secret:db/password}", secret.Reference{Key: "db/password"}},
		{"${secret:db/creds:password}", secret.Reference{Key: "db/creds", Field: "password"}},
		{"${secret:db/creds:password:3}", secret.Reference{Key: "db/creds", Field: "password", Version: "3"}},
		{"${secret:db/creds::3}", secret.Reference{Key: "db/creds", Version: "3"}},
		{"${secret@vault:platform/signing:key}", secret.Reference{Provider: "vault", Key: "platform/signing", Field: "key"}},
		{"${secret@aws-production:payments/api-key::AWSCURRENT}", secret.Reference{Provider: "aws-production", Key: "payments/api-key", Version: "AWSCURRENT"}},
		{"${secret@gcp:analytics-token}", secret.Reference{Provider: "gcp", Key: "analytics-token"}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := secret.ParseReference(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseReference_RoundTripsCanonically(t *testing.T) {
	for _, input := range []string{
		"${secret:db/password}",
		"${secret:db/creds:password}",
		"${secret:db/creds:password:3}",
		"${secret:db/creds::3}",
		"${secret@vault:platform/signing:key}",
		"${secret@aws-production:payments/api-key::AWSCURRENT}",
	} {
		t.Run(input, func(t *testing.T) {
			ref, err := secret.ParseReference(input)
			require.NoError(t, err)
			assert.Equal(t, input, ref.String(), "serialization must round-trip exactly")

			again, err := secret.ParseReference(ref.String())
			require.NoError(t, err)
			assert.Equal(t, ref, again, "re-parsing the canonical form must be stable")
		})
	}
}

func TestParseReference_RejectsTrailingSyntax(t *testing.T) {
	for _, input := range []string{
		"${secret:db/password} ",
		" ${secret:db/password}",
		"${secret:db/password}extra",
		"prefix${secret:db/password}",
		"${secret:db/password}${secret:other}",
	} {
		t.Run(input, func(t *testing.T) {
			_, err := secret.ParseReference(input)
			require.Error(t, err, "a reference must occupy the whole input")
			var refErr *secret.ReferenceError
			assert.ErrorAs(t, err, &refErr)
		})
	}
}

func TestParseReference_RejectsMalformed(t *testing.T) {
	for name, input := range map[string]string{
		"empty":            "",
		"not a reference":  "plain-value",
		"no key":           "${secret:}",
		"no closing brace": "${secret:db/password",
		"wrong scheme":     "${notsecret:db/password}",
		"empty provider":   "${secret@:db/password}",
		"too many parts":   "${secret:a:b:c:d}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := secret.ParseReference(input)
			require.Error(t, err)
		})
	}
}

// A trailing colon supplies an empty field, which is the pre-existing grammar
// and means the same as supplying no field. Canonical serialization drops it,
// so parsing is lenient about the input form while String is not.
func TestParseReference_TrailingColonIsAnEmptyField(t *testing.T) {
	ref, err := secret.ParseReference("${secret:db/password:}")
	require.NoError(t, err, "an empty field is valid; the grammar predates this API")
	assert.Equal(t, secret.Reference{Key: "db/password"}, ref)
	assert.Equal(t, "${secret:db/password}", ref.String(),
		"canonical form drops the redundant separator")
}

func TestReferenceError_NamesTheLocatorNotAValue(t *testing.T) {
	_, err := secret.ParseReference("${secret:db/password")
	require.Error(t, err)

	var refErr *secret.ReferenceError
	require.ErrorAs(t, err, &refErr)
	assert.Contains(t, err.Error(), "db/password",
		"the error must identify what failed to parse")
	assert.NotEmpty(t, refErr.Reason, "the error must say why")
}

func TestParseReference_ContactsNoProvider(t *testing.T) {
	// A parse is pure syntax. A provider alias that is not registered anywhere
	// still parses: routing is resolved later, by the configuration.
	ref, err := secret.ParseReference("${secret@no-such-provider:key}")
	require.NoError(t, err)
	assert.Equal(t, "no-such-provider", ref.Provider)
}

func TestReference_StringOfZeroValue(t *testing.T) {
	assert.Empty(t, secret.Reference{}.String(),
		"a reference with no key has no canonical form")
}

func TestContainsReference(t *testing.T) {
	assert.True(t, secret.ContainsReference("url: postgres://u:${secret:db/password}@host/db"))
	assert.True(t, secret.ContainsReference("${secret@vault:k}"))
	assert.False(t, secret.ContainsReference("no references here"))
	assert.False(t, secret.ContainsReference("${env:HOME}"))
}

func TestFindReferences(t *testing.T) {
	refs := secret.FindReferences(
		"postgres://admin:${secret:db/password}@host/${secret@vault:db/name:value}")
	require.Len(t, refs, 2)
	assert.Equal(t, secret.Reference{Key: "db/password"}, refs[0])
	assert.Equal(t, secret.Reference{Provider: "vault", Key: "db/name", Field: "value"}, refs[1])
}

// The grammar is delimiter-based with no escape mechanism, so a component
// containing a delimiter is not representable. That is a real limitation and
// the parser must not pretend otherwise by silently truncating.
func TestParseReference_DelimitersAreNotRepresentable(t *testing.T) {
	_, err := secret.ParseReference("${secret:key:with:too:many:colons}")
	require.Error(t, err, "colons in a component must be rejected, never truncated")
}

// A Resolver holds one store and performs no routing. When a value mixes an
// unqualified reference with a provider-qualified one, resolving the qualified
// one against the only store available would return a value from the wrong
// backend without saying so. It is reported instead.
func TestResolver_RejectsProviderQualifiedReferenceAlongsideUnqualified(t *testing.T) {
	r := secret.NewResolver(secret.NewDictStore(map[string]any{
		"a": "value-a",
		"b": "from-default-store",
	}))

	in := "${secret:a} and ${secret@vault:b}"
	out, err := r.Resolve(context.Background(), in)
	require.Error(t, err)
	assert.ErrorIs(t, err, secret.ErrProviderRoutingUnsupported)
	assert.Equal(t, in, out, "a failed resolution returns the input unchanged")
	assert.NotContains(t, err.Error(), "from-default-store",
		"the error must not carry a resolved value")
	assert.NotContains(t, err.Error(), "value-a",
		"the error must not carry an earlier successful value")
}

// A provider-qualified reference is rejected however it appears. An earlier
// revision checked only for the unqualified prefix before scanning, so a value
// carrying a qualified reference alone was returned verbatim with a nil error,
// publishing what looks like a live reference as a literal configuration value
// while the same reference beside an unqualified one produced the typed error.
func TestResolver_RejectsProviderQualifiedReferenceAlone(t *testing.T) {
	r := secret.NewResolver(secret.NewDictStore(map[string]any{"key": "from-default-store"}))

	in := "${secret@vault:key}"
	out, err := r.Resolve(context.Background(), in)
	require.Error(t, err, "a qualified reference alone must not pass silently")
	assert.ErrorIs(t, err, secret.ErrProviderRoutingUnsupported)
	assert.Equal(t, in, out, "the input comes back unchanged")
	assert.NotContains(t, err.Error(), "from-default-store",
		"the error must not carry a resolved value")
}

func TestResolver_StillResolvesUnqualifiedReference(t *testing.T) {
	r := secret.NewResolver(secret.NewDictStore(map[string]any{"key": "value"}))

	out, err := r.Resolve(context.Background(), "${secret:key}")
	require.NoError(t, err)
	assert.Equal(t, "value", out)
}
