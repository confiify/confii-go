// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

// Behavior tests for the HashiCorp Vault secret store.
//
// Provider: HashiCorp Vault, KV v2 backend (secret/cloud/vault.go,
// secret/cloud/vault_auth.go).
//
// Fixture mechanism: protocol-level fixture via `httptest.NewServer`
// that mimics the Vault HTTP API. The store is constructed with
// `WithVaultURL(server.URL)` and `WithVaultToken(...)`, which are
// passed verbatim to `api.NewClient`. This keeps the test hermetic
// (no real Vault binary, no Docker) while still exercising every
// production code path that builds the request URL, threads the auth
// token, parses the JSON response, and maps errors to the confii
// sentinel set.
//
// Build-tag gating: this file is gated by `//go:build vault`; the cloud
// module owns the required Vault SDK version. Upstream CI exercises it
// through the same temporary consumer shape users install.
//
// Running locally as a consumer:
//
//	go test -tags vault -count=1 ./...

package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go"
)

// vaultFixture wires a httptest server up as a fake Vault endpoint.
// The handler is a simple path-based mux populated by the test; if no
// route matches the path, the fixture responds 404 — which is the same
// shape Vault uses for "secret not found" on a KV v2 read.
type vaultFixture struct {
	server *httptest.Server
	routes map[string]func(http.ResponseWriter, *http.Request)
}

func newVaultFixture(t *testing.T) *vaultFixture {
	t.Helper()
	f := &vaultFixture{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := f.routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *vaultFixture) handle(path string, h func(http.ResponseWriter, *http.Request)) {
	f.routes[path] = h
}

// TestVault_GetSecret_HappyPath_KVv2_Map asserts the documented contract
// for KV v2 reads: the store reaches the `secret/data/<path>` endpoint,
// peels the `data.data` envelope, and returns the inner map.
func TestVault_GetSecret_HappyPath_KVv2_Map(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/secret/data/myapp/db", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Vault-Token"); got != "dev-root-token" {
			http.Error(w, "bad token: "+got, http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": "abc",
			"data": map[string]any{
				"data": map[string]any{
					"username": "alice",
					"password": "hunter2",
				},
				"metadata": map[string]any{"version": 3},
			},
		})
	})

	store, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
		WithVaultKVVersion(2),
		WithVaultMountPoint("secret"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	got, err := store.GetSecret(context.Background(), "myapp/db")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}
	if m["username"] != "alice" || m["password"] != "hunter2" {
		t.Errorf("data: got %#v, want {username:alice,password:hunter2}", m)
	}
}

// TestVault_GetSecret_HappyPath_FieldExtraction asserts that the
// "path:field" syntax pulls a single field from the KV v2 envelope.
func TestVault_GetSecret_HappyPath_FieldExtraction(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/secret/data/myapp/db", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]any{"password": "hunter2"},
			},
		})
	})

	store, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	got, err := store.GetSecret(context.Background(), "myapp/db:password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("GetSecret: got %#v, want %q", got, "hunter2")
	}
}

// TestVault_GetSecret_FieldNotFound surfaces ErrSecretValidation when
// the requested field is absent from a successful KV v2 read.
func TestVault_GetSecret_FieldNotFound(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/secret/data/myapp/db", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]any{"username": "alice"},
			},
		})
	})

	store, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	_, err = store.GetSecret(context.Background(), "myapp/db:password")
	if err == nil {
		t.Fatal("expected ErrSecretValidation, got nil")
	}
	if !errors.Is(err, confii.ErrSecretValidation) {
		t.Errorf("error: got %v, want wrapping ErrSecretValidation", err)
	}
}

// TestVault_GetSecret_NotFound asserts that a 404 from Vault surfaces
// as ErrSecretNotFound, not as a generic access error. Vault's HTTP
// API returns 404 for missing KV v2 entries. The fixture deliberately uses
// net/http's plain-text 404 body to prove status mapping happens before JSON
// decoding; proxies and development servers do not always return Vault's
// usual JSON error envelope.
func TestVault_GetSecret_NotFound(t *testing.T) {
	f := newVaultFixture(t)
	// No routes registered — the default handler responds 404.

	store, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	_, err = store.GetSecret(context.Background(), "missing/secret")
	if err == nil {
		t.Fatal("expected ErrSecretNotFound, got nil")
	}
	if !errors.Is(err, confii.ErrSecretNotFound) {
		t.Errorf("error: got %v, want wrapping ErrSecretNotFound", err)
	}
}

// TestVault_GetSecret_ServerError surfaces ErrSecretAccess for 500
// responses, distinguishing transient backend failures from missing
// secrets.
func TestVault_GetSecret_ServerError(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/secret/data/myapp/db", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":["internal error"]}`))
	})

	store, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	_, err = store.GetSecret(context.Background(), "myapp/db")
	if err == nil {
		t.Fatal("expected ErrSecretAccess, got nil")
	}
	if errors.Is(err, confii.ErrSecretNotFound) {
		t.Errorf("error: got ErrSecretNotFound, want ErrSecretAccess for 500: %v", err)
	}
	if !errors.Is(err, confii.ErrSecretAccess) {
		t.Errorf("error: got %v, want wrapping ErrSecretAccess", err)
	}
}

// TestVault_GetSecret_KVv1_PathShape exercises the KV-v1 code path:
// the URL must NOT include the `/data/` segment and the response body
// is the secret payload directly (no `data.data` envelope).
func TestVault_GetSecret_KVv1_PathShape(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/secret/myapp/db", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"password": "kv1-secret"},
		})
	})

	store, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
		WithVaultKVVersion(1),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	got, err := store.GetSecret(context.Background(), "myapp/db:password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "kv1-secret" {
		t.Errorf("GetSecret: got %#v, want %q", got, "kv1-secret")
	}
}

// TestVault_AppRoleLogin_HappyPath verifies the constructor's AppRole
// auth path: when `WithVaultAppRole` is configured, NewHashiCorpVault
// posts to `auth/approle/login` and stores the returned client_token.
// We then issue a GetSecret to confirm the token is threaded into
// subsequent requests.
func TestVault_AppRoleLogin_HappyPath(t *testing.T) {
	f := newVaultFixture(t)
	var loginCalled bool
	f.handle("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalled = true
		body := struct {
			RoleID   string `json:"role_id"`
			SecretID string `json:"secret_id"`
		}{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.RoleID != "the-role" || body.SecretID != "the-secret" {
			http.Error(w, "bad approle creds", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth": map[string]any{
				"client_token": "minted-approle-token",
				"renewable":    true,
				"lease_id":     "",
			},
		})
	})
	f.handle("/v1/secret/data/myapp/db", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Vault-Token"); got != "minted-approle-token" {
			http.Error(w, "wrong token: "+got, http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"data": map[string]any{"password": "ok"}},
		})
	})

	store, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultAppRole("the-role", "the-secret"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}
	if !loginCalled {
		t.Fatal("expected AppRole login to be called during construction")
	}

	got, err := store.GetSecret(context.Background(), "myapp/db:password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "ok" {
		t.Errorf("GetSecret: got %#v, want %q", got, "ok")
	}
}

// TestVault_AppRoleLogin_BadCredentials asserts that a failed AppRole
// login surfaces as ErrVaultAuth, not as a generic store error. This
// is the auth-path regression guard for G27 (skeletal Vault auth) —
// even though the production code path for the named Auth methods is
// thin, the explicit AppRole path through NewHashiCorpVault must keep
// the documented error contract.
func TestVault_AppRoleLogin_BadCredentials(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":["invalid role or secret id"]}`))
	})

	_, err := NewHashiCorpVault(
		WithVaultURL(f.server.URL),
		WithVaultAppRole("bad-role", "bad-secret"),
	)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
	if !strings.Contains(err.Error(), "approle login") {
		t.Errorf("error message: got %q, want containing 'approle login'", err.Error())
	}
}

// TestVault_TokenAuth_AuthenticateReturnsToken sanity-checks the
// TokenAuth method: it should return its configured token verbatim
// without contacting Vault. This is the only Vault auth method that
// does not need a network round-trip; everything else is exercised by
// TestVault_AppRoleLogin_* above.
func TestVault_TokenAuth_AuthenticateReturnsToken(t *testing.T) {
	auth := &TokenAuth{Token: "static-token"}
	got, err := auth.Authenticate(nil)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "static-token" {
		t.Errorf("Authenticate: got %q, want %q", got, "static-token")
	}
}
