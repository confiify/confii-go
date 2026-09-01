// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedStore returns a chosen value for every key, so a test can place
// exactly the material a template needs to form a new placeholder.
type scriptedStore struct {
	value any
	reads int
}

func (s *scriptedStore) GetSecret(context.Context, string, ...confii.SecretOption) (any, error) {
	s.reads++
	return s.value, nil
}
func (s *scriptedStore) SetSecret(context.Context, string, any, ...confii.SecretOption) error {
	return nil
}
func (s *scriptedStore) DeleteSecret(context.Context, string, ...confii.SecretOption) error {
	return nil
}
func (s *scriptedStore) ListSecrets(context.Context, string) ([]string, error) { return nil, nil }

// A resolved value ending in '$' completes a '{...}' sequence in the text that
// follows it, manufacturing a reference the author never wrote. Neither the
// value nor the template is dangerous alone; the seam between them is.
func TestResolve_RejectsReferenceSynthesizedAtASeam(t *testing.T) {
	store := &scriptedStore{value: "trailing$"}
	r := NewResolver(store, WithCache(false))

	in := "${secret:a}{secret:b:c}"
	out, err := r.Resolve(context.Background(), in)

	require.Error(t, err, "a substitution that manufactures a reference must not succeed")
	assert.ErrorIs(t, err, confii.ErrSecretValidation)
	assert.Equal(t, in, out, "a failed resolution returns the input unchanged")
	assert.NotContains(t, err.Error(), "trailing$",
		"the error must not carry resolved material")
}

// The same defect reached through the value itself rather than a seam: a
// secret whose value is a reference would otherwise be resolvable a second
// time, chaining one secret read into another.
func TestResolve_RejectsReferenceCarriedInAResolvedValue(t *testing.T) {
	store := &scriptedStore{value: "${secret:escalated}"}
	r := NewResolver(store, WithCache(false))

	out, err := r.Resolve(context.Background(), "${secret:entry}")

	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrSecretValidation)
	assert.Equal(t, "${secret:entry}", out)
	assert.NotContains(t, err.Error(), "escalated",
		"the error must not name the secret the value tried to reach")
	assert.Equal(t, 1, store.reads,
		"the synthesized reference must never be read from the store")
}

// Values that merely contain placeholder-looking fragments are fine as long as
// no complete reference results. The guard must reject synthesis, not any '$'.
func TestResolve_AllowsHarmlessDollarsAndBraces(t *testing.T) {
	for name, value := range map[string]string{
		"trailing dollar alone": "ends-with$",
		"brace only":            "has{brace}",
		"dollar brace no key":   "${",
		"incomplete scheme":     "${secret",
		"shell style":           "$HOME/path",
	} {
		t.Run(name, func(t *testing.T) {
			r := NewResolver(&scriptedStore{value: value}, WithCache(false))
			out, err := r.Resolve(context.Background(), "prefix ${secret:a} suffix")
			require.NoError(t, err, "value %q must resolve without complaint", value)
			assert.Contains(t, out, value)
		})
	}
}

func TestResolve_OutputNeverContainsAResolvableReference(t *testing.T) {
	// Whatever the store returns, a successful resolution must leave nothing
	// behind that a further pass would resolve. That is the property the
	// synthesis guard exists to hold.
	for _, value := range []string{"plain", "ends$", "{braces}", "a${b", "x}y"} {
		r := NewResolver(&scriptedStore{value: value}, WithCache(false))
		out, err := r.Resolve(context.Background(), "${secret:a}{secret:b}")
		if err != nil {
			continue // rejected, which is also acceptable
		}
		assert.False(t, secretPattern.MatchString(out),
			"resolved output %q still holds a resolvable reference", out)
	}
}

func TestResolve_IsIdempotentAfterTheGuard(t *testing.T) {
	r := NewResolver(&scriptedStore{value: "safe-value"}, WithCache(false))

	first, err := r.Resolve(context.Background(), "a ${secret:k} b")
	require.NoError(t, err)
	second, err := r.Resolve(context.Background(), first)
	require.NoError(t, err)
	assert.Equal(t, first, second, "resolution must converge")
	assert.False(t, strings.Contains(second, "${secret:"))
}

// A template may reference the same key more than once. The failure message
// should name each locator once rather than repeating it, so a wide template
// does not produce an error dominated by duplicates.
func TestResolve_SynthesisErrorNamesEachLocatorOnce(t *testing.T) {
	r := NewResolver(&scriptedStore{value: "trailing$"}, WithCache(false))

	_, err := r.Resolve(context.Background(), "${secret:dup} ${secret:dup}{secret:x}")
	require.Error(t, err)
	assert.Equal(t, 1, strings.Count(err.Error(), `"dup"`),
		"a repeated locator must appear once in the error")
}
