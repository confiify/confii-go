// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	confii "github.com/confiify/confii-go/v2"
)

// DictStore is a concurrency-safe in-memory store intended for tests and local
// development. It provides no encryption, persistence, access control, or
// defensive copying of stored composite values and is unsuitable for
// production secrets.
type DictStore struct {
	mu       sync.RWMutex
	secrets  map[string]any
	versions map[string][]any
}

// NewDictStore creates a store containing initial. The outer map is copied, but
// nested maps and slices remain shared with the caller.
func NewDictStore(initial map[string]any) *DictStore {
	secrets := make(map[string]any)
	for k, v := range initial {
		secrets[k] = v
	}
	return &DictStore{
		secrets:  secrets,
		versions: make(map[string][]any),
	}
}

// GetSecret retrieves a secret value from the in-memory store by key.
//
// When opts include WithVersion, the version string must be a non-negative
// integer index into the recorded version history for the key. A negative
// index, an out-of-range index, an unparseable version string, or a key with
// no recorded version history all yield ErrSecretNotFound; these inputs
// intentionally do NOT fall through to the current ("latest") value, because
// silently substituting the current secret for a caller that explicitly asked
// for a specific version is a correctness hazard.
func (s *DictStore) GetSecret(_ context.Context, key string, opts ...confii.SecretOption) (any, error) {
	o := confii.ResolveSecretOptions(opts...)
	s.mu.RLock()
	defer s.mu.RUnlock()

	if o.Version != "" {
		idx, err := strconv.Atoi(o.Version)
		if err != nil {
			return nil, fmt.Errorf("%w: %s (invalid version %q)", confii.ErrSecretNotFound, key, o.Version)
		}
		versions, ok := s.versions[key]
		if !ok {
			return nil, fmt.Errorf("%w: %s (no version history)", confii.ErrSecretNotFound, key)
		}
		if idx < 0 || idx >= len(versions) {
			return nil, fmt.Errorf("%w: %s (version %d out of range [0,%d))", confii.ErrSecretNotFound, key, idx, len(versions))
		}
		return versions[idx], nil
	}

	val, ok := s.secrets[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
	}
	return val, nil
}

// SetSecret stores value as the current value and appends it to zero-based
// version history. Secret options are ignored. Composite values are retained
// by reference.
func (s *DictStore) SetSecret(_ context.Context, key string, value any, _ ...confii.SecretOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[key] = append(s.versions[key], value)
	s.secrets[key] = value
	return nil
}

// DeleteSecret removes a secret and its version history from the in-memory store.
func (s *DictStore) DeleteSecret(_ context.Context, key string, _ ...confii.SecretOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, key)
	delete(s.versions, key)
	return nil
}

// ListSecrets returns keys whose names begin with prefix, or all keys for an
// empty prefix. Ordering is unspecified.
func (s *DictStore) ListSecrets(_ context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for k := range s.secrets {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Len returns the number of secrets stored.
func (s *DictStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.secrets)
}

// Clear removes all current values and version history.
func (s *DictStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = make(map[string]any)
	s.versions = make(map[string][]any)
}
