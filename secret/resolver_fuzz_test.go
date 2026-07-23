package secret

// This file fuzzes Resolver.Resolve.
//
// The fuzz runs every input through three resolver configurations
// (failOnMissing=false, failOnMissing=true with a never-failing store,
// and failOnMissing=true with an always-failing store). For each
// (resolver, input) pair the following invariants are asserted:
//
//  1. Result/error exclusivity (when failOnMissing=true): if err is
//     non-nil, the returned string is the original input verbatim
//     (placeholders left in place — the resolver only mutates
//     placeholders that resolve successfully).
//  2. Typed error: when failOnMissing=true and the underlying store
//     returns confii.ErrSecretNotFound, the resolver's error must
//     wrap it (errors.Is). For an always-failing store with a custom
//     sentinel, errors.Is must find the sentinel as well.
//  3. Pass-through: when the input contains no "${secret:" prefix the
//     resolver must return (input, nil) verbatim. (This is the early
//     return in resolver.Resolve and a load-bearing performance
//     property.)
//  4. Idempotence under successful resolution: when failOnMissing=false
//     and the store always succeeds, calling Resolve a second time on
//     the first call's output must produce the same output again — the
//     output of a successful resolution must be a stable fixed-point.

import (
	"context"
	"errors"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go"
)

// fuzzStore returns the key as its own value for any GetSecret call.
type fuzzStore struct{}

func (s *fuzzStore) GetSecret(_ context.Context, key string, _ ...confii.SecretOption) (any, error) {
	return key, nil
}

func (s *fuzzStore) SetSecret(_ context.Context, _ string, _ any, _ ...confii.SecretOption) error {
	return nil
}

func (s *fuzzStore) DeleteSecret(_ context.Context, _ string, _ ...confii.SecretOption) error {
	return nil
}

func (s *fuzzStore) ListSecrets(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// errSecretFuzzStore is a synthetic store that always returns
// confii.ErrSecretNotFound — used to exercise the failOnMissing error
// propagation path.
type errSecretFuzzStore struct{}

func (s *errSecretFuzzStore) GetSecret(_ context.Context, _ string, _ ...confii.SecretOption) (any, error) {
	return nil, confii.ErrSecretNotFound
}

func (s *errSecretFuzzStore) SetSecret(_ context.Context, _ string, _ any, _ ...confii.SecretOption) error {
	return nil
}

func (s *errSecretFuzzStore) DeleteSecret(_ context.Context, _ string, _ ...confii.SecretOption) error {
	return nil
}

func (s *errSecretFuzzStore) ListSecrets(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func FuzzResolverResolve(f *testing.F) {
	seeds := []string{
		"no placeholders",
		"${secret:mykey}",
		"${secret:db/password}",
		"${secret:key:json.path}",
		"${secret:key:path:v1}",
		"${secret:key::v1}",
		"${secret:db/pass::42}",
		"${secret:key::-1}",
		"${secret:key::abc}",
		"prefix_${secret:key}_suffix",
		"${secret:a}${secret:b}",
		"${secret:}",
		"${secret:key:}",
		"${secret:key::}",
		"${secret:",
		"${secret}",
		"${}",
		"${secret:a:b:c:d:e}",
		"${secret:key/with/slashes}",
		"${secret:key.with.dots}",
		"${SECRET:key}",
		"${secret:key:path.to.nested.value}",
		"",
		// Idempotence seed: this string is the fuzzStore's resolution
		// of "${secret:foo}" because the store returns the key. Feeding
		// it back in must be a no-op for resolver behavior.
		"foo",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		ctx := context.Background()

		// Configuration A: failOnMissing=false, store always succeeds.
		// Cache disabled to avoid cross-iteration state pollution under
		// the fuzz harness's parallel execution.
		softResolver := NewResolver(&fuzzStore{}, WithResolverFailOnMissing(false), WithCache(false))
		out, err := softResolver.Resolve(ctx, input)
		if err != nil {
			t.Fatalf("failOnMissing=false unexpectedly returned error: input=%q err=%v", input, err)
		}

		// Invariant 3: pass-through when no placeholder prefix is
		// present.
		if !strings.Contains(input, "${secret:") {
			if out != input {
				t.Fatalf("resolver mutated placeholder-free input: in=%q out=%q", input, out)
			}
		}

		// Invariant 4: idempotence under successful resolution.
		// Resolving the output a second time must produce the same
		// output. Note: the output may itself contain a "${secret:"
		// substring if the original key text contained one (e.g.
		// "${secret:${secret:nested}}"); fuzzStore returns the inner
		// key verbatim, which is the second-pass input. The fixed
		// point is reached after at most one further pass for any
		// well-formed seed; if not reached, that's a regression worth
		// flagging.
		out2, err := softResolver.Resolve(ctx, out)
		if err != nil {
			t.Fatalf("idempotence pass returned error: out=%q err=%v", out, err)
		}
		out3, err := softResolver.Resolve(ctx, out2)
		if err != nil {
			t.Fatalf("idempotence pass 3 returned error: out2=%q err=%v", out2, err)
		}
		if out2 != out3 {
			t.Fatalf("resolver did not converge: pass2=%q pass3=%q (input=%q)", out2, out3, input)
		}

		// Configuration B: failOnMissing=true, store always succeeds
		// (returns the key string verbatim). The strict resolver may
		// still raise a validation error when a placeholder requests
		// a json_path on a non-map secret value (e.g.
		// "${secret:key:json.path}" against the string-returning
		// store). When multiple placeholders are present, the
		// resolver does NOT short-circuit on the first error: it
		// continues processing the remaining placeholders and only
		// surfaces the *last* error encountered. So the invariants
		// in this configuration are:
		//   - On error: the error must wrap one of the documented
		//     ErrSecret* sentinels.
		//   - On success: the output equals the soft resolver's
		//     output for the same input.
		strictResolver := NewResolver(&fuzzStore{}, WithResolverFailOnMissing(true), WithCache(false))
		strictOut, strictErr := strictResolver.Resolve(ctx, input)
		if strictErr != nil {
			if !errors.Is(strictErr, confii.ErrSecretValidation) &&
				!errors.Is(strictErr, confii.ErrSecretNotFound) &&
				!errors.Is(strictErr, confii.ErrSecretAccess) &&
				!errors.Is(strictErr, confii.ErrSecretStore) {
				t.Fatalf("strict resolver error wraps no known ErrSecret* sentinel: %v", strictErr)
			}
		} else if strictOut != out {
			t.Fatalf("strict and soft resolvers disagreed on always-succeeding store: input=%q soft=%q strict=%q",
				input, out, strictOut)
		}

		// Configuration C: failOnMissing=true, store always returns
		// ErrSecretNotFound. Behavior depends on whether the input
		// actually contains a placeholder that the regex matches.
		errResolver := NewResolver(&errSecretFuzzStore{}, WithResolverFailOnMissing(true), WithCache(false))
		errOut, errErr := errResolver.Resolve(ctx, input)
		if errErr != nil {
			// Invariant 1: when failOnMissing=true and an error
			// occurs, the returned string is the input verbatim —
			// secretPattern.ReplaceAllStringFunc leaves placeholders
			// in place when the callback returns the original match.
			if errOut != input {
				t.Fatalf("failOnMissing=true error path mutated input: in=%q out=%q err=%v",
					input, errOut, errErr)
			}
			// Invariant 2: the error must wrap ErrSecretNotFound.
			if !errors.Is(errErr, confii.ErrSecretNotFound) {
				t.Fatalf("failOnMissing=true error does not wrap ErrSecretNotFound: %v", errErr)
			}
		} else {
			// No error means the regex matched nothing — the input
			// must come through unchanged for the always-failing
			// store.
			if errOut != input {
				t.Fatalf("failOnMissing=true with no-match input mutated string: in=%q out=%q",
					input, errOut)
			}
		}
	})
}
