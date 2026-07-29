// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

package cloud

import (
	"context"
	"fmt"

	confii "github.com/confiify/confii-go"
)

func init() {
	confii.RegisterSelfConfigSecretProvider("azure", func(cfg map[string]any) (confii.SelfConfigSecretStore, error) {
		vaultURL := selfString(cfg, "vault_url", "address", "url")
		if vaultURL == "" {
			return nil, fmt.Errorf("azure provider requires vault_url")
		}
		store, err := NewAzureKeyVault(vaultURL, nil)
		if err != nil {
			return nil, err
		}
		return selfConfigStoreAdapter{get: func(ctx context.Context, key string, secretOpts ...confii.SecretOption) (any, error) {
			return store.GetSecret(ctx, key, secretOpts...)
		}}, nil
	})
}
