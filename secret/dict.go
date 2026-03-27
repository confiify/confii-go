package secret

import (
	"context"
	"fmt"
	"strings"
	"sync"

	confii "github.com/qualitycoe/confii-go"
)

// DictStore is an in-memory secret store for testing and development.
type DictStore struct {
	mu       sync.RWMutex
	secrets  map[string]any
	versions map[string][]any
}

// NewDictStore creates a new in-memory secret store.
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

func (s *DictStore) GetSecret(_ context.Context, key string, opts ...confii.SecretOption) (any, error) {
	o := confii.ResolveSecretOptions(opts...)
	s.mu.RLock()
	defer s.mu.RUnlock()

	if o.Version != "" {
		if versions, ok := s.versions[key]; ok {
			idx := 0
			if _, err := fmt.Sscanf(o.Version, "%d", &idx); err == nil && idx < len(versions) {
				return versions[idx], nil
			}
		}
	}

	val, ok := s.secrets[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
	}
	return val, nil
}

func (s *DictStore) SetSecret(_ context.Context, key string, value any, _ ...confii.SecretOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[key] = append(s.versions[key], value)
	s.secrets[key] = value
	return nil
}

func (s *DictStore) DeleteSecret(_ context.Context, key string, _ ...confii.SecretOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, key)
	delete(s.versions, key)
	return nil
}

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

// Clear removes all secrets.
func (s *DictStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets = make(map[string]any)
	s.versions = make(map[string][]any)
}
