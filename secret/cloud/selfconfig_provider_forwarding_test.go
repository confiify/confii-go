// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws && azure && gcp && vault

package cloud

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
)

type versionedSelfConfigStore interface {
	GetSecretRequest(context.Context, confii.SelfConfigSecretRequest) (any, error)
}

func TestCloudSelfConfigFactoriesForwardVersionedRequests(t *testing.T) {
	t.Run("aws", func(t *testing.T) {
		store := buildRegisteredCloudStore(t, "aws", map[string]any{
			"region": "us-east-1", "access_key": "test", "secret_key": "test",
			"endpoint": "http://127.0.0.1:1",
		})
		assertVersionedRequestReachesStore(t, store, "secret-id")
	})

	t.Run("azure", func(t *testing.T) {
		t.Setenv("AZURE_TENANT_ID", "00000000-0000-0000-0000-000000000000")
		t.Setenv("AZURE_CLIENT_ID", "00000000-0000-0000-0000-000000000001")
		t.Setenv("AZURE_CLIENT_SECRET", "test")
		store := buildRegisteredCloudStore(t, "azure", map[string]any{
			"vault_url": "https://example.vault.azure.net",
		})
		assertVersionedRequestReachesStore(t, store, "invalid/name")
	})

	t.Run("gcp", func(t *testing.T) {
		credentialsPath := filepath.Join(t.TempDir(), "credentials.json")
		if err := os.WriteFile(credentialsPath, []byte(`{
  "type": "authorized_user",
  "client_id": "test-client",
  "client_secret": "test-secret",
  "refresh_token": "test-refresh"
}`), 0o600); err != nil {
			t.Fatalf("write GCP credentials: %v", err)
		}
		store := buildRegisteredCloudStore(t, "gcp", map[string]any{
			"project_id": "test-project", "credentials_file": credentialsPath,
		})
		assertVersionedRequestReachesStore(t, store, "secret-id")
	})

	t.Run("vault", func(t *testing.T) {
		store := buildRegisteredCloudStore(t, "vault", map[string]any{
			"address": "http://127.0.0.1:1", "verify": false, "token": "test-token",
		})
		assertVersionedRequestReachesStore(t, store, "secret-id")
	})
}

func buildRegisteredCloudStore(t *testing.T, provider string, cfg map[string]any) confii.SelfConfigSecretStore {
	t.Helper()
	factory, ok := confii.LookupSelfConfigSecretProvider(provider)
	if !ok {
		t.Fatalf("provider %s must be registered", provider)
	}
	store, err := factory(cfg)
	if err != nil {
		t.Fatalf("create %s store: %v", provider, err)
	}
	return store
}

func assertVersionedRequestReachesStore(t *testing.T, store confii.SelfConfigSecretStore, key string) {
	t.Helper()
	versioned, ok := store.(versionedSelfConfigStore)
	if !ok {
		t.Fatalf("store %T does not support versioned secret requests", store)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := versioned.GetSecretRequest(ctx, confii.SelfConfigSecretRequest{Key: key, Version: "7"})
	if err == nil {
		t.Fatal("expected canceled versioned request to fail")
	}
}
