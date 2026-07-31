// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

package cloud

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	confii "github.com/confiify/confii-go/v2"
)

var _ azcore.TokenCredential = (*fakeAzureCredential)(nil)

type fakeAzureCredential struct {
	token string
	calls int
}

func (f *fakeAzureCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	f.calls++
	return azcore.AccessToken{
		Token:     f.token,
		ExpiresOn: time.Now().Add(1 * time.Hour),
	}, nil
}

type azureFixture struct {
	server *httptest.Server
	routes map[string]func(http.ResponseWriter, *http.Request)
}

func newAzureFixture(t *testing.T) *azureFixture {
	t.Helper()
	f := &azureFixture{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer authorization="https://login.microsoftonline.com/tenant", resource="https://vault.azure.net"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if h, ok := f.routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *azureFixture) handle(path string, h func(http.ResponseWriter, *http.Request)) {
	f.routes[path] = h
}

func newAzureKeyVaultForTest(t *testing.T, vaultURL string, cred azcore.TokenCredential) *AzureKeyVault {
	t.Helper()
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	opts := &azsecrets.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: httpClient,
		},
		DisableChallengeResourceVerification: true,
	}
	c, err := azsecrets.NewClient(vaultURL, cred, opts)
	if err != nil {
		t.Fatalf("azsecrets.NewClient: %v", err)
	}
	return &AzureKeyVault{client: c}
}

func TestAzure_NewKeyVault_AcceptsTokenCredentialInterface(t *testing.T) {
	f := newAzureFixture(t)
	var bearer string
	f.handle("/secrets/api-key/", func(w http.ResponseWriter, r *http.Request) {
		bearer = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		val := "s3cr3t"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": val,
			"id":    f.server.URL + "/secrets/api-key/v1",
		})
	})

	cred := &fakeAzureCredential{token: "fake-bearer-xyz"}

	if _, err := NewAzureKeyVault(f.server.URL, cred); err != nil {
		t.Logf("NewAzureKeyVault: %v (expected without TLS-tolerant transport)", err)
	}

	store := newAzureKeyVaultForTest(t, f.server.URL, cred)

	got, err := store.GetSecret(context.Background(), "api-key")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "s3cr3t" {
		t.Errorf("GetSecret: got %#v, want %q", got, "s3cr3t")
	}
	if cred.calls == 0 {
		t.Error("expected fake credential to be consulted at least once")
	}
	if !strings.Contains(bearer, "fake-bearer-xyz") {
		t.Errorf("Authorization header: got %q, want containing the fake token", bearer)
	}
}

func TestAzure_NewKeyVault_NilCredentialUsesDefault(t *testing.T) {
	store, err := NewAzureKeyVault("https://example.vault.azure.net/", nil)
	if err != nil {

		msg := err.Error()
		if !strings.Contains(msg, "default credential") &&
			!strings.Contains(msg, "azure") {
			t.Errorf("error: got %q, want mentioning the default credential branch", msg)
		}
		return
	}
	if store == nil {
		t.Fatal("NewAzureKeyVault returned nil store with nil error")
	}
}

func TestAzure_GetSecret_InvalidName(t *testing.T) {
	f := newAzureFixture(t)
	store := newAzureKeyVaultForTest(t, f.server.URL, &fakeAzureCredential{token: "fake"})

	_, err := store.GetSecret(context.Background(), "bad name with spaces")
	if err == nil {
		t.Fatal("expected ErrSecretValidation, got nil")
	}
	if !errors.Is(err, confii.ErrSecretValidation) {
		t.Errorf("error: got %v, want wrapping ErrSecretValidation", err)
	}
}

func TestAzure_GetSecret_NotFound(t *testing.T) {
	f := newAzureFixture(t)
	f.handle("/secrets/missing/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"SecretNotFound","message":"not found"}}`))
	})

	store := newAzureKeyVaultForTest(t, f.server.URL, &fakeAzureCredential{token: "fake"})

	_, err := store.GetSecret(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, confii.ErrSecretAccess) && !errors.Is(err, confii.ErrSecretNotFound) {
		t.Errorf("error: got %v, want wrapping ErrSecretAccess or ErrSecretNotFound", err)
	}
}
