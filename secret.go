// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import "context"

// SecretStore is the common read/write contract for a secret-management
// backend. Implementations must honor context cancellation, be safe for the
// concurrency they advertise, and return [ErrSecretNotFound] only for genuine
// absence. Authentication, authorization, transport, and backend failures
// should wrap [ErrSecretAccess] or [ErrSecretStore] and must not be converted
// into not-found results.
type SecretStore interface {
	// GetSecret returns key, optionally selecting a version or structured field.
	GetSecret(ctx context.Context, key string, opts ...SecretOption) (any, error)
	// SetSecret creates or replaces key with value when supported by the backend.
	SetSecret(ctx context.Context, key string, value any, opts ...SecretOption) error
	// DeleteSecret removes key or returns an implementation-specific error.
	DeleteSecret(ctx context.Context, key string, opts ...SecretOption) error
	// ListSecrets returns keys beneath prefix. Ordering is backend-specific.
	ListSecrets(ctx context.Context, prefix string) ([]string, error)
}

// SecretExistenceChecker is an optional consumer-facing capability for stores
// that can check existence without retrieving secret material. Applications
// may feature-detect it with a type assertion; Confii's resolution pipeline
// requires only [SecretStore].
type SecretExistenceChecker interface {
	// SecretExists reports whether key exists without returning its value.
	SecretExists(ctx context.Context, key string) (bool, error)
}

// SecretMetadataProvider is an optional consumer-facing capability for stores
// that expose value-safe secret metadata. Applications may feature-detect it
// with a type assertion; implementations must not include secret values in the
// returned map.
type SecretMetadataProvider interface {
	// GetSecretMetadata returns backend-specific metadata for key.
	GetSecretMetadata(ctx context.Context, key string) (map[string]any, error)
}

// SecretOption configures optional secret operation parameters.
type SecretOption func(*SecretOptions)

// SecretOptions holds backend-neutral selectors for a secret operation. Empty
// fields request the backend's current version and complete value.
type SecretOptions struct {
	// Version selects a backend-defined immutable or historical version.
	Version string
	// Field selects one member from a structured secret value.
	Field string
}

// WithVersion selects a backend-defined secret version. An empty value clears
// a previously supplied version option. Backends that do not support versions
// should return an explicit error rather than silently ignoring it.
func WithVersion(v string) SecretOption {
	return func(o *SecretOptions) { o.Version = v }
}

// WithField selects a field from a structured secret value. An empty value
// requests the complete value. A missing field should return
// [ErrSecretNotFound] or a backend-specific error that wraps it.
func WithField(field string) SecretOption {
	return func(o *SecretOptions) { o.Field = field }
}

// ResolveSecretOptions applies opts in order and returns an independent value;
// later options override earlier values. A nil option is invalid and panics in
// the same manner as invoking a nil function.
func ResolveSecretOptions(opts ...SecretOption) SecretOptions {
	var o SecretOptions
	for _, fn := range opts {
		fn(&o)
	}
	return o
}
