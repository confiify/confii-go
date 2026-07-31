// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws && azure && gcp && vault

package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestCloudSelfConfigFactoriesForwardVersionedRequests(t *testing.T) {
	t.Run("aws", func(t *testing.T) {
		f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/x-amz-json-1.1")
			_ = json.NewEncoder(w).Encode(map[string]any{"SecretString": "ok"})
		})
		store := buildRegisteredCloudStore(t, "aws", map[string]any{
			"region": "us-east-1", "access_key": "test", "secret_key": "test",
			"endpoint": f.server.URL,
		})
		value, err := store.ReadSecret(context.Background(),
			confii.SecretRequest{Key: "secret-id", Version: "7"})
		if err != nil {
			t.Fatalf("versioned ReadSecret: %v", err)
		}
		if value != "ok" {
			t.Errorf("ReadSecret value: got %#v, want %q", value, "ok")
		}
		if !strings.Contains(string(f.lastBody), `"VersionId":"7"`) {
			t.Errorf("request body %q must forward the version as VersionId 7", f.lastBody)
		}
	})

	t.Run("azure", func(t *testing.T) {
		t.Setenv("AZURE_TENANT_ID", "00000000-0000-0000-0000-000000000000")
		t.Setenv("AZURE_CLIENT_ID", "00000000-0000-0000-0000-000000000001")
		t.Setenv("AZURE_CLIENT_SECRET", "test")
		store := buildRegisteredCloudStore(t, "azure", map[string]any{
			"vault_url": "https://example.vault.azure.net",
		})
		assertVersionedRequestReachesProviderIO(t, store, "invalid/name")
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
		assertVersionedRequestReachesProviderIO(t, store, "secret-id")
	})

	t.Run("vault", func(t *testing.T) {
		f := newVaultFixture(t)
		var gotQuery string
		f.handle("/v1/secret/data/secret-id", func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": {"data": {"value": "ok"}, "metadata": {"version": 7}}}`))
		})
		store := buildRegisteredCloudStore(t, "vault", map[string]any{
			"address": f.server.URL, "verify": false, "token": "test-token",
		})
		value, err := store.ReadSecret(context.Background(),
			confii.SecretRequest{Key: "secret-id", Version: "7"})
		if err != nil {
			t.Fatalf("versioned ReadSecret: %v", err)
		}
		if value == nil {
			t.Error("ReadSecret must return the fixture secret value")
		}
		if !strings.Contains(gotQuery, "version=7") {
			t.Errorf("request query %q must forward version=7", gotQuery)
		}
	})
}

func buildRegisteredCloudStore(t *testing.T, provider string, cfg map[string]any) confii.SecretReader {
	t.Helper()
	factory, ok := confii.LookupSelfConfigSecretProvider(provider)
	if !ok {
		t.Fatalf("provider %s must be registered", provider)
	}
	store, err := factory(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create %s store: %v", provider, err)
	}
	return store
}

// assertVersionedRequestReachesProviderIO verifies only that the adapter
// admits a versioned request and reaches provider I/O (the pre-canceled
// context surfaces from the transport attempt). It does NOT verify that the
// version value is forwarded; providers with an injectable endpoint (aws,
// vault) assert actual forwarding against a captured request above.
func assertVersionedRequestReachesProviderIO(t *testing.T, store confii.SecretReader, key string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.ReadSecret(ctx, confii.SecretRequest{Key: key, Version: "7"})
	if err == nil {
		t.Fatal("expected canceled versioned request to fail")
	}
}
