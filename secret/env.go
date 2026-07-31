// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package secret provides secret store implementations and a placeholder resolver.
package secret

import (
	"context"
	"fmt"
	"os"
	"strings"

	confii "github.com/confiify/confii-go/v2"
)

// EnvStore retrieves secrets from environment variables.
type EnvStore struct {
	prefix       string
	suffix       string
	transformKey bool
}

// EnvStoreOption configures an EnvStore.
type EnvStoreOption func(*EnvStore)

// WithEnvPrefix sets a prefix prepended to env var names.
func WithEnvPrefix(p string) EnvStoreOption {
	return func(s *EnvStore) { s.prefix = p }
}

// WithEnvSuffix sets a suffix appended to env var names.
func WithEnvSuffix(s string) EnvStoreOption {
	return func(st *EnvStore) { st.suffix = s }
}

// WithTransformKey controls whether "/", ".", and "-" are replaced with "_"
// and the result uppercased. Transformation is enabled by default.
func WithTransformKey(v bool) EnvStoreOption {
	return func(s *EnvStore) { s.transformKey = v }
}

// NewEnvStore creates a process-environment store. It offers no encryption and
// may expose values to child processes or process-inspection tools; use it only
// where the deployment environment provides appropriate isolation.
func NewEnvStore(opts ...EnvStoreOption) *EnvStore {
	s := &EnvStore{transformKey: true}
	for _, o := range opts {
		o(s)
	}
	return s
}

// GetSecret reads the transformed key with configured prefix and suffix. A
// missing variable wraps [confii.ErrSecretNotFound]. Secret options are
// ignored because process variables are neither structured nor versioned.
func (s *EnvStore) GetSecret(_ context.Context, key string, _ ...confii.SecretOption) (any, error) {
	envKey := s.envKey(key)
	val, ok := os.LookupEnv(envKey)
	if !ok {
		return nil, fmt.Errorf("%w: env var %s not found", confii.ErrSecretNotFound, envKey)
	}
	return val, nil
}

// SetSecret sets a secret by writing the value to the corresponding OS environment variable.
func (s *EnvStore) SetSecret(_ context.Context, key string, value any, _ ...confii.SecretOption) error {
	return os.Setenv(s.envKey(key), fmt.Sprintf("%v", value))
}

// DeleteSecret removes a secret by unsetting the corresponding OS environment variable.
func (s *EnvStore) DeleteSecret(_ context.Context, key string, _ ...confii.SecretOption) error {
	return os.Unsetenv(s.envKey(key))
}

// ListSecrets returns raw process-environment variable names beginning with
// prefix, or every name when prefix is empty. The store's configured key
// transform, prefix, and suffix are not reversed. Ordering follows os.Environ
// and should not be relied upon.
func (s *EnvStore) ListSecrets(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for _, env := range os.Environ() {
		k, _, _ := strings.Cut(env, "=")
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *EnvStore) envKey(key string) string {
	if s.transformKey {
		key = strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(key)
		key = strings.ToUpper(key)
	}
	return s.prefix + key + s.suffix
}
