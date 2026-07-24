// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestVaultSelfConfigProviderRegistered(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("vault")
	if !ok {
		t.Fatal("vault self-config secret provider was not registered")
	}
	store, err := factory(map[string]any{
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
