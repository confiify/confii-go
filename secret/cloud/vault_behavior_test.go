// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

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

	store, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
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

func TestNewOpenBao_UsesVaultCompatibleImplementation(t *testing.T) {
	store, err := NewOpenBao(WithVaultURL("http://127.0.0.1:8200"),
		WithVaultToken("test-token"),
	)
	if err != nil {
		t.Fatalf("NewOpenBao: %v", err)
	}
	if store == nil {
		t.Fatal("NewOpenBao returned a nil store")
	}
	if store.mountPoint != "secret" || store.kvVersion != 2 {
		t.Fatalf("defaults = mount %q, KV v%d; want secret, KV v2", store.mountPoint, store.kvVersion)
	}
}

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

	store, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	got, err := store.GetSecret(context.Background(), "myapp/db", confii.WithField("password"))
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("GetSecret: got %#v, want %q", got, "hunter2")
	}
}

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

	store, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	_, err = store.GetSecret(context.Background(), "myapp/db", confii.WithField("password"))
	if err == nil {
		t.Fatal("expected ErrSecretValidation, got nil")
	}
	if !errors.Is(err, confii.ErrSecretValidation) {
		t.Errorf("error: got %v, want wrapping ErrSecretValidation", err)
	}
}

func TestVault_GetSecret_NotFound(t *testing.T) {
	f := newVaultFixture(t)

	store, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
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

func TestVault_GetSecret_ServerError(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/secret/data/myapp/db", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":["internal error"]}`))
	})

	store, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
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

func TestVault_GetSecret_KVv1_PathShape(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/secret/myapp/db", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"password": "kv1-secret"},
		})
	})

	store, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
		WithVaultToken("dev-root-token"),
		WithVaultKVVersion(1),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}

	got, err := store.GetSecret(context.Background(), "myapp/db", confii.WithField("password"))
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "kv1-secret" {
		t.Errorf("GetSecret: got %#v, want %q", got, "kv1-secret")
	}
}

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

	store, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
		WithVaultAppRole("the-role", "the-secret"),
	)
	if err != nil {
		t.Fatalf("NewHashiCorpVault: %v", err)
	}
	if !loginCalled {
		t.Fatal("expected AppRole login to be called during construction")
	}

	got, err := store.GetSecret(context.Background(), "myapp/db", confii.WithField("password"))
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "ok" {
		t.Errorf("GetSecret: got %#v, want %q", got, "ok")
	}
}

func TestVault_AppRoleLogin_BadCredentials(t *testing.T) {
	f := newVaultFixture(t)
	f.handle("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":["invalid role or secret id"]}`))
	})

	_, err := NewHashiCorpVault(WithVaultURL(f.server.URL),
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

func TestVault_TokenAuth_AuthenticateReturnsToken(t *testing.T) {
	auth := &TokenAuth{Token: "static-token"}
	got, err := auth.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "static-token" {
		t.Errorf("Authenticate: got %q, want %q", got, "static-token")
	}
}
