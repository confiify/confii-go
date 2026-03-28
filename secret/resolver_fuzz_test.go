package secret

import (
	"context"
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

func FuzzResolverResolve(f *testing.F) {
	seeds := []string{
		"no placeholders",
		"${secret:mykey}",
		"${secret:db/password}",
		"${secret:key:json.path}",
		"${secret:key:path:v1}",
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
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		resolver := NewResolver(&fuzzStore{}, WithResolverFailOnMissing(false))
		// Must not panic for any input.
		_, _ = resolver.Resolve(context.Background(), input)
	})
}
