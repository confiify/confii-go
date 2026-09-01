// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws || azure || gcp || vault

package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	confii "github.com/confiify/confii-go/v2"
)

type selfConfigStoreAdapter struct {
	get func(context.Context, string, ...confii.SecretOption) (any, error)
	// close releases the underlying store. Without it the store a provider
	// built would never be closed: Config closes resources implementing
	// Close() error, and the resource it holds is this adapter, not the store
	// the adapter closed over.
	close func() error
}

// Close releases the underlying store when the provider supplied a closer.
// Providers whose stores own nothing leave it nil, and closing is then a no-op.
func (s selfConfigStoreAdapter) Close() error {
	if s.close == nil {
		return nil
	}
	return s.close()
}

func (s selfConfigStoreAdapter) ReadSecret(ctx context.Context, request confii.SecretRequest) (any, error) {
	var opts []confii.SecretOption
	if request.Version != "" {
		opts = append(opts, confii.WithVersion(request.Version))
	}
	value, err := s.get(ctx, request.Key, opts...)
	if err != nil {
		return nil, err
	}
	return extractDeclarativeSecretField(value, request.Field)
}

func extractDeclarativeSecretField(value any, field string) (any, error) {
	if field == "" {
		return value, nil
	}
	if encoded, ok := value.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			return nil, fmt.Errorf("%w: cannot traverse field %q in non-structured secret value", confii.ErrSecretValidation, field)
		}
		value = decoded
	}
	current := value
	for _, part := range strings.Split(field, ".") {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: cannot traverse field %q in non-map secret value", confii.ErrSecretValidation, field)
		}
		current, ok = mapping[part]
		if !ok {
			return nil, fmt.Errorf("%w: field %q not found in secret", confii.ErrSecretValidation, field)
		}
	}
	return current, nil
}

func selfString(cfg map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := cfg[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
