// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

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

		"foo",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		ctx := context.Background()

		softResolver := NewResolver(&fuzzStore{}, WithCache(false))
		out, err := softResolver.Resolve(ctx, input)
		if err != nil {
			if !errors.Is(err, confii.ErrSecretValidation) {
				t.Fatalf("resolver returned unexpected error:input=%q err=%v", input, err)
			}
			return
		}

		if !strings.Contains(input, "${secret:") {
			if out != input {
				t.Fatalf("resolver mutated placeholder-free input: in=%q out=%q", input, out)
			}
		}

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

		strictResolver := NewResolver(&fuzzStore{}, WithCache(false))
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

		errResolver := NewResolver(&errSecretFuzzStore{}, WithCache(false))
		errOut, errErr := errResolver.Resolve(ctx, input)
		if errErr != nil {

			if errOut != input {
				t.Fatalf("failOnMissing=true error path mutated input: in=%q out=%q err=%v",
					input, errOut, errErr)
			}

			if !errors.Is(errErr, confii.ErrSecretNotFound) {
				t.Fatalf("failOnMissing=true error does not wrap ErrSecretNotFound: %v", errErr)
			}
		} else {

			if errOut != input {
				t.Fatalf("failOnMissing=true with no-match input mutated string: in=%q out=%q",
					input, errOut)
			}
		}
	})
}
