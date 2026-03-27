package confii

import "context"

// SecretStore provides access to a secret management backend.
type SecretStore interface {
	GetSecret(ctx context.Context, key string, opts ...SecretOption) (any, error)
	SetSecret(ctx context.Context, key string, value any, opts ...SecretOption) error
	DeleteSecret(ctx context.Context, key string, opts ...SecretOption) error
	ListSecrets(ctx context.Context, prefix string) ([]string, error)
}

// SecretExistenceChecker is optionally implemented by stores that can
// efficiently check for secret existence without retrieving the value.
type SecretExistenceChecker interface {
	SecretExists(ctx context.Context, key string) (bool, error)
}

// SecretMetadataProvider is optionally implemented by stores that can
// return metadata about a secret.
type SecretMetadataProvider interface {
	GetSecretMetadata(ctx context.Context, key string) (map[string]any, error)
}

// SecretOption configures optional secret operation parameters.
type SecretOption func(*SecretOptions)

// SecretOptions holds resolved secret operation options.
type SecretOptions struct {
	Version string
}

// WithVersion sets the secret version for retrieval.
func WithVersion(v string) SecretOption {
	return func(o *SecretOptions) { o.Version = v }
}

// ResolveSecretOptions applies all options and returns the resolved result.
func ResolveSecretOptions(opts ...SecretOption) SecretOptions {
	var o SecretOptions
	for _, fn := range opts {
		fn(&o)
	}
	return o
}
