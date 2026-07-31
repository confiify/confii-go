// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/hashicorp/vault/api"
)

type authFixture struct {
	server   *httptest.Server
	routes   map[string]func(http.ResponseWriter, *http.Request)
	lastBody map[string]any
	lastPath string
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	f := &authFixture{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastPath = r.URL.Path
		buf, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = json.Unmarshal(buf, &f.lastBody)

		r.Body = io.NopCloser(strings.NewReader(string(buf)))
		if h, ok := f.routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *authFixture) handle(path string, h func(http.ResponseWriter, *http.Request)) {
	f.routes[path] = h
}

func (f *authFixture) client(t *testing.T) *api.Client {
	t.Helper()
	cfg := api.DefaultConfig()
	cfg.Address = f.server.URL
	c, err := api.NewClient(cfg)
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	return c
}

func writeAuthOK(w http.ResponseWriter, token string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"auth": map[string]any{
			"client_token": token,
			"renewable":    true,
		},
	})
}

func TestVaultAuth_TokenAuth_RejectsEmptyToken(t *testing.T) {
	auth := &TokenAuth{}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

func TestVaultAuth_LDAP_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/ldap/login/alice", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "ldap-minted-token")
	})

	auth := &LDAPAuth{Username: "alice", Password: "hunter2"}
	got, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "ldap-minted-token" {
		t.Errorf("token: got %q, want %q", got, "ldap-minted-token")
	}
	if f.lastBody["password"] != "hunter2" {
		t.Errorf("body password: got %v, want hunter2", f.lastBody["password"])
	}
}

func TestVaultAuth_LDAP_PasswordProvider(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/ldap/login/alice", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "ldap-provider-token")
	})
	called := false
	auth := &LDAPAuth{
		Username: "alice",
		PasswordProvider: func(context.Context) (string, error) {
			called = true
			return "provided-password", nil
		},
	}
	_, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !called {
		t.Error("PasswordProvider was not consulted")
	}
	if f.lastBody["password"] != "provided-password" {
		t.Errorf("body password: got %v, want provided-password", f.lastBody["password"])
	}
}

func TestVaultAuth_LDAP_MissingPassword(t *testing.T) {
	auth := &LDAPAuth{Username: "alice"}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

func TestVaultAuth_JWT_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/jwt/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "jwt-minted-token")
	})

	auth := &JWTAuth{Role: "demo", JWT: "eyJ..."}
	got, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "jwt-minted-token" {
		t.Errorf("token: got %q, want %q", got, "jwt-minted-token")
	}
	if f.lastBody["role"] != "demo" || f.lastBody["jwt"] != "eyJ..." {
		t.Errorf("body: got %v, want role=demo jwt=eyJ...", f.lastBody)
	}
}

func TestVaultAuth_JWT_MissingFields(t *testing.T) {
	auth := &JWTAuth{Role: "demo"}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

func TestVaultAuth_Kubernetes_ReadsTokenFromFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("k8s-projected-jwt"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	f := newAuthFixture(t)
	f.handle("/v1/auth/kubernetes/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "k8s-minted-token")
	})

	auth := &KubernetesAuth{Role: "demo", TokenPath: tokenFile}
	got, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "k8s-minted-token" {
		t.Errorf("token: got %q, want %q", got, "k8s-minted-token")
	}
	if f.lastBody["jwt"] != "k8s-projected-jwt" {
		t.Errorf("body jwt: got %v, want k8s-projected-jwt", f.lastBody["jwt"])
	}
}

func TestVaultAuth_Kubernetes_MissingTokenFile(t *testing.T) {
	auth := &KubernetesAuth{Role: "demo", TokenPath: "/no/such/path/token"}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

func TestVaultAuth_AWSIAM_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/aws/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "iam-minted-token")
	})

	signedURL := base64.StdEncoding.EncodeToString([]byte("https://sts.amazonaws.com/"))
	signedBody := base64.StdEncoding.EncodeToString([]byte("Action=GetCallerIdentity&Version=2011-06-15"))
	signedHeaders := base64.StdEncoding.EncodeToString([]byte(`{"Authorization":["AWS4-HMAC-SHA256 ..."]}`))

	auth := &AWSIAMSignedRequestAuth{
		Role:                  "demo",
		IAMHTTPRequestMethod:  "POST",
		IAMHTTPRequestURL:     signedURL,
		IAMHTTPRequestBody:    signedBody,
		IAMHTTPRequestHeaders: signedHeaders,
	}
	got, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "iam-minted-token" {
		t.Errorf("token: got %q, want %q", got, "iam-minted-token")
	}
	if f.lastBody["iam_http_request_method"] != "POST" {
		t.Errorf("body method: got %v, want POST", f.lastBody["iam_http_request_method"])
	}
	if f.lastBody["iam_request_url"] != signedURL {
		t.Errorf("body url: got %v, want %v", f.lastBody["iam_request_url"], signedURL)
	}
}

func TestVaultAuth_AWSIAMSignedRequest_IncompleteRequestRejected(t *testing.T) {
	auth := &AWSIAMSignedRequestAuth{Role: "demo"}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

func TestVaultAuth_Azure_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/azure/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "azure-minted-token")
	})

	auth := &AzureJWTAuth{
		Role:           "demo",
		JWT:            "eyJAzure...",
		SubscriptionID: "00000000-0000-0000-0000-000000000000",
	}
	got, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "azure-minted-token" {
		t.Errorf("token: got %q, want %q", got, "azure-minted-token")
	}
	if f.lastBody["jwt"] != "eyJAzure..." {
		t.Errorf("body jwt: got %v, want eyJAzure...", f.lastBody["jwt"])
	}
	if f.lastBody["subscription_id"] != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("body subscription_id: got %v", f.lastBody["subscription_id"])
	}
}

func TestVaultAuth_AzureJWT_MissingJWT(t *testing.T) {
	auth := &AzureJWTAuth{Role: "demo"}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

func TestVaultAuth_GCP_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/gcp/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "gcp-minted-token")
	})

	auth := &JWTAuth{Role: "demo", JWT: "eyJGCP...", MountPoint: "gcp"}
	got, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got != "gcp-minted-token" {
		t.Errorf("token: got %q, want %q", got, "gcp-minted-token")
	}
	if f.lastBody["jwt"] != "eyJGCP..." {
		t.Errorf("body jwt: got %v, want eyJGCP...", f.lastBody["jwt"])
	}
}

func TestVaultAuth_GCPJWT_MissingJWT(t *testing.T) {
	auth := &JWTAuth{Role: "demo", MountPoint: "gcp"}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

func TestVaultAuth_OIDC_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/custom-oidc/oidc/auth_url", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"auth_url": "https://idp.example/authorize?state=vault-state&nonce=vault-nonce",
		}})
	})
	f.handle("/v1/auth/custom-oidc/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != "vault-state" || q.Get("nonce") != "vault-nonce" || q.Get("code") != "provider-code" {
			t.Errorf("callback query: got %v", q)
		}
		if q.Get("client_nonce") != "client-nonce" {
			t.Errorf("client_nonce: got %q", q.Get("client_nonce"))
		}
		writeAuthOK(w, "oidc-minted-token")
	})

	auth := &OIDCAuth{
		Role:        "demo",
		MountPoint:  "custom-oidc",
		RedirectURI: "http://localhost:8250/oidc/callback",
		ClientNonce: "client-nonce",
		CallbackProvider: func(_ context.Context, authURL string) (string, error) {
			if !strings.Contains(authURL, "state=vault-state") {
				t.Errorf("authorization URL: got %q", authURL)
			}

			return "http://localhost:8250/oidc/callback?state=vault-state&code=provider-code", nil
		},
	}
	token, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if token != "oidc-minted-token" {
		t.Errorf("token: got %q, want oidc-minted-token", token)
	}
}

func TestVaultAuth_OIDC_RejectsMismatchedState(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/oidc/oidc/auth_url", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"auth_url": "https://idp.example/authorize?state=expected&nonce=expected-nonce",
		}})
	})
	auth := &OIDCAuth{
		ClientNonce: "client-nonce",
		CallbackProvider: func(context.Context, string) (string, error) {
			return "http://localhost:8250/oidc/callback?state=attacker&code=code", nil
		},
	}
	_, err := auth.Authenticate(context.Background(), f.client(t))
	if err == nil || !errors.Is(err, confii.ErrVaultAuth) {
		t.Fatalf("error: got %v, want ErrVaultAuth", err)
	}
	if !strings.Contains(err.Error(), "state did not match") {
		t.Errorf("error: got %v, want state mismatch", err)
	}
}

func TestVaultAuth_OIDC_BuiltInLoopbackCallback(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback unavailable: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oidc/callback", port)

	f := newAuthFixture(t)
	f.handle("/v1/auth/oidc/oidc/auth_url", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"auth_url": "https://idp.example/authorize?state=loopback-state&nonce=loopback-nonce",
		}})
	})
	f.handle("/v1/auth/oidc/oidc/callback", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "loopback-token")
	})

	auth := &OIDCAuth{
		RedirectURI: redirectURI,
		ClientNonce: "loopback-client-nonce",
		OpenBrowser: func(string) error {
			callback := redirectURI + "?state=loopback-state&code=loopback-code"
			resp, getErr := http.Get(callback)
			if resp != nil {
				_ = resp.Body.Close()
			}
			return getErr
		},
	}
	token, err := auth.Authenticate(context.Background(), f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if token != "loopback-token" {
		t.Fatalf("token: got %q, want loopback-token", token)
	}
}

func TestVaultAuth_AppRole_RejectsEmpty(t *testing.T) {
	auth := &AppRoleAuth{}
	_, err := auth.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}
