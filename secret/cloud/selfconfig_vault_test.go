// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestVaultSelfConfigProviderRegistered(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("vault")
	if !ok {
		t.Fatal("vault self-config secret provider was not registered")
	}
	store, err := factory(context.Background(), map[string]any{
		"address": "http://127.0.0.1:8200",
		"token":   "test-token",
	})
	if err != nil {
		t.Fatalf("build registered Vault provider: %v", err)
	}
	if store == nil {
		t.Fatal("registered Vault provider returned a nil store")
	}
}
