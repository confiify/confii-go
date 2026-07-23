//go:build vault

// Behavior tests for the HashiCorp Vault auth method implementations.
//
// Provider: HashiCorp Vault auth backends (secret/cloud/vault_auth.go).
//
// Fixture mechanism: protocol-level fixture via `httptest.NewServer`
// that mimics the Vault auth REST endpoints. Each test points the Vault
// `*api.Client` at the fixture's URL via `api.DefaultConfig()` +
// `cfg.Address = server.URL`, then drives the auth method's
// Authenticate method end-to-end. We assert both the request shape
// (path, mount point, body fields) and the response handling (token
// extraction, error wrapping).
//
// G26 coverage focus: the previous implementations of AWSIAMAuth,
// AzureAuth, GCPAuth, and OIDCAuth submitted only role/resource fields
// and did not validate that the consumer had supplied the
// credential/assertion the SDK actually needs. This file covers each
// auth method, including the complete OIDC auth_url/callback exchange.
//
// Build-tag gating: //go:build vault, same rationale as
// vault_behavior_test.go.

package cloud

import (
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

	confii "github.com/confiify/confii-go"
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
		// Re-attach the body so the route handler can re-decode if needed.
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

// TestVaultAuth_TokenAuth_RejectsEmptyToken — pre-G26 the implementation
// returned the empty string silently; after the fix it surfaces a typed
// error.
func TestVaultAuth_TokenAuth_RejectsEmptyToken(t *testing.T) {
	auth := &TokenAuth{}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

// TestVaultAuth_LDAP_HappyPath drives the LDAPAuth login through the
// fixture and asserts the canonical mount path + payload shape.
func TestVaultAuth_LDAP_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/ldap/login/alice", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "ldap-minted-token")
	})

	auth := &LDAPAuth{Username: "alice", Password: "hunter2"}
	got, err := auth.Authenticate(f.client(t))
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

// TestVaultAuth_LDAP_PasswordProvider asserts that PasswordProvider is
// consulted when Password is empty.
func TestVaultAuth_LDAP_PasswordProvider(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/ldap/login/alice", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "ldap-provider-token")
	})
	called := false
	auth := &LDAPAuth{
		Username: "alice",
		PasswordProvider: func() (string, error) {
			called = true
			return "provided-password", nil
		},
	}
	_, err := auth.Authenticate(f.client(t))
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

// TestVaultAuth_LDAP_MissingPassword surfaces ErrVaultAuth without
// hitting the network.
func TestVaultAuth_LDAP_MissingPassword(t *testing.T) {
	auth := &LDAPAuth{Username: "alice"}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

// TestVaultAuth_JWT_HappyPath drives the JWTAuth login.
func TestVaultAuth_JWT_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/jwt/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "jwt-minted-token")
	})

	auth := &JWTAuth{Role: "demo", JWT: "eyJ..."}
	got, err := auth.Authenticate(f.client(t))
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

// TestVaultAuth_JWT_MissingFields surfaces ErrVaultAuth without hitting
// the network.
func TestVaultAuth_JWT_MissingFields(t *testing.T) {
	auth := &JWTAuth{Role: "demo"}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

// TestVaultAuth_Kubernetes_ReadsTokenFromFile asserts the in-pod token
// file discovery: when JWT is empty, KubernetesAuth reads from TokenPath.
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
	got, err := auth.Authenticate(f.client(t))
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

// TestVaultAuth_Kubernetes_MissingTokenFile surfaces ErrVaultAuth.
func TestVaultAuth_Kubernetes_MissingTokenFile(t *testing.T) {
	auth := &KubernetesAuth{Role: "demo", TokenPath: "/no/such/path/token"}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}

// TestVaultAuth_AWSIAM_HappyPath asserts the signed-STS payload flows
// through to Vault when all four IAMHTTPRequest* fields are populated.
func TestVaultAuth_AWSIAM_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/aws/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "iam-minted-token")
	})

	signedURL := base64.StdEncoding.EncodeToString([]byte("https://sts.amazonaws.com/"))
	signedBody := base64.StdEncoding.EncodeToString([]byte("Action=GetCallerIdentity&Version=2011-06-15"))
	signedHeaders := base64.StdEncoding.EncodeToString([]byte(`{"Authorization":["AWS4-HMAC-SHA256 ..."]}`))

	auth := &AWSIAMAuth{
		Role:                  "demo",
		IAMHTTPRequestMethod:  "POST",
		IAMHTTPRequestURL:     signedURL,
		IAMHTTPRequestBody:    signedBody,
		IAMHTTPRequestHeaders: signedHeaders,
	}
	got, err := auth.Authenticate(f.client(t))
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

// TestVaultAuth_AWSIAM_UnsignedRequestRejected asserts that the typed
// error is surfaced when the consumer hasn't signed the STS request.
func TestVaultAuth_AWSIAM_UnsignedRequestRejected(t *testing.T) {
	auth := &AWSIAMAuth{Role: "demo"}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
	if !errors.Is(err, ErrVaultAuthUnsupported) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuthUnsupported", err)
	}
}

// TestVaultAuth_Azure_HappyPath drives AzureAuth with an in-hand JWT.
func TestVaultAuth_Azure_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/azure/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "azure-minted-token")
	})

	auth := &AzureAuth{
		Role:           "demo",
		JWT:            "eyJAzure...",
		SubscriptionID: "00000000-0000-0000-0000-000000000000",
	}
	got, err := auth.Authenticate(f.client(t))
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

// TestVaultAuth_Azure_MissingJWT surfaces the typed unsupported error.
func TestVaultAuth_Azure_MissingJWT(t *testing.T) {
	auth := &AzureAuth{Role: "demo"}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
	if !errors.Is(err, ErrVaultAuthUnsupported) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuthUnsupported", err)
	}
}

// TestVaultAuth_GCP_HappyPath drives GCPAuth with a pre-signed JWT.
func TestVaultAuth_GCP_HappyPath(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/gcp/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "gcp-minted-token")
	})

	auth := &GCPAuth{Role: "demo", JWT: "eyJGCP..."}
	got, err := auth.Authenticate(f.client(t))
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

// TestVaultAuth_GCP_MissingJWT surfaces the typed unsupported error.
func TestVaultAuth_GCP_MissingJWT(t *testing.T) {
	auth := &GCPAuth{Role: "demo"}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
	if !errors.Is(err, ErrVaultAuthUnsupported) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuthUnsupported", err)
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
		CallbackProvider: func(authURL string) (string, error) {
			if !strings.Contains(authURL, "state=vault-state") {
				t.Errorf("authorization URL: got %q", authURL)
			}
			return "http://localhost:8250/oidc/callback?state=vault-state&nonce=vault-nonce&code=provider-code", nil
		},
	}
	token, err := auth.Authenticate(f.client(t))
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
		CallbackProvider: func(string) (string, error) {
			return "http://localhost:8250/oidc/callback?state=attacker&nonce=expected-nonce&code=code", nil
		},
	}
	_, err := auth.Authenticate(f.client(t))
	if err == nil || !errors.Is(err, confii.ErrVaultAuth) {
		t.Fatalf("error: got %v, want ErrVaultAuth", err)
	}
	if !strings.Contains(err.Error(), "state or nonce") {
		t.Errorf("error: got %v, want state/nonce mismatch", err)
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
			callback := redirectURI + "?state=loopback-state&nonce=loopback-nonce&code=loopback-code"
			resp, getErr := http.Get(callback)
			if resp != nil {
				_ = resp.Body.Close()
			}
			return getErr
		},
	}
	token, err := auth.Authenticate(f.client(t))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if token != "loopback-token" {
		t.Fatalf("token: got %q, want loopback-token", token)
	}
}

// TestVaultAuth_AppRole_RejectsEmpty asserts the new validation guard
// without contacting the network.
func TestVaultAuth_AppRole_RejectsEmpty(t *testing.T) {
	auth := &AppRoleAuth{}
	_, err := auth.Authenticate(nil)
	if err == nil {
		t.Fatal("expected ErrVaultAuth, got nil")
	}
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Errorf("error: got %v, want wrapping ErrVaultAuth", err)
	}
}
