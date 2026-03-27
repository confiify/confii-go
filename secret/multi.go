package secret

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	confii "github.com/confiify/confii-go"
)

// MultiStore tries multiple stores in priority order.
type MultiStore struct {
	stores        []confii.SecretStore
	failOnMissing bool
	writeToFirst  bool
	logger        *slog.Logger
}

// MultiStoreOption configures a MultiStore.
type MultiStoreOption func(*MultiStore)

// WithFailOnMissing controls whether a missing secret across all stores is an error.
func WithFailOnMissing(v bool) MultiStoreOption {
	return func(s *MultiStore) { s.failOnMissing = v }
}

// WithWriteToFirst controls whether writes go only to the first store.
func WithWriteToFirst(v bool) MultiStoreOption {
	return func(s *MultiStore) { s.writeToFirst = v }
}

// NewMultiStore creates a store that tries each store in order.
func NewMultiStore(stores []confii.SecretStore, opts ...MultiStoreOption) *MultiStore {
	s := &MultiStore{
		stores:        stores,
		failOnMissing: true,
		writeToFirst:  true,
		logger:        slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// GetSecret tries each store in priority order, returning the first successful result.
func (s *MultiStore) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	for _, store := range s.stores {
		val, err := store.GetSecret(ctx, key, opts...)
		if err == nil {
			return val, nil
		}
		if !errors.Is(err, confii.ErrSecretNotFound) {
			s.logger.Warn("secret store error", slog.String("key", key), slog.String("error", err.Error()))
		}
	}
	if s.failOnMissing {
		return nil, fmt.Errorf("%w: %s (tried %d stores)", confii.ErrSecretNotFound, key, len(s.stores))
	}
	return nil, nil
}

// SetSecret writes a secret to the first store or all stores, depending on configuration.
func (s *MultiStore) SetSecret(ctx context.Context, key string, value any, opts ...confii.SecretOption) error {
	if s.writeToFirst && len(s.stores) > 0 {
		return s.stores[0].SetSecret(ctx, key, value, opts...)
	}
	for _, store := range s.stores {
		if err := store.SetSecret(ctx, key, value, opts...); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSecret removes a secret from the first store or all stores, depending on configuration.
func (s *MultiStore) DeleteSecret(ctx context.Context, key string, opts ...confii.SecretOption) error {
	if s.writeToFirst && len(s.stores) > 0 {
		return s.stores[0].DeleteSecret(ctx, key, opts...)
	}
	for _, store := range s.stores {
		if err := store.DeleteSecret(ctx, key, opts...); err != nil {
			return err
		}
	}
	return nil
}

// ListSecrets aggregates secret keys from all stores, deduplicating the results.
func (s *MultiStore) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	seen := make(map[string]struct{})
	var result []string
	for _, store := range s.stores {
		keys, err := store.ListSecrets(ctx, prefix)
		if err != nil {
			continue
		}
		for _, k := range keys {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				result = append(result, k)
			}
		}
	}
	return result, nil
}
