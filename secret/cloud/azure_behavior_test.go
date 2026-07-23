//go:build azure

// Behavior tests for the Azure Key Vault secret store.
//
// Provider: Azure Key Vault (secret/cloud/azure.go).
//
// Fixture mechanism: protocol-level fixture via `httptest.NewTLSServer`
// that mimics the Azure Key Vault REST endpoint. The Azure SDK refuses
// to send authenticated requests to non-TLS endpoints, so the fixture
// terminates TLS with httptest's self-signed cert and we feed the
// fixture's `*http.Client` (which trusts that cert) to the SDK via
// `azsecrets.ClientOptions.Transport`. The store then runs end-to-end
// without any global env-var hackery and without a real Azure tenant.
//
// G26 coverage focus: the constructor's `credential` parameter was
// previously typed as `any` but only accepted
// `*azidentity.DefaultAzureCredential` at runtime. After the fix it is
// typed as [azcore.TokenCredential], so any implementation of the SDK
// interface — including the test fakes below — drives the production
// code without runtime gymnastics. The compile-time interface
// acceptance is itself part of the regression guard:
// `var _ azcore.TokenCredential = (*fakeAzureCredential)(nil)` proves
// the test fake satisfies the interface, and passing it to
// [NewAzureKeyVault] proves the constructor accepts it.
//
// Build-tag gating: this file is gated by `//go:build azure`; the cloud
// module owns the required Azure SDK versions. Upstream CI exercises it
// through the same temporary consumer shape users install.
//
// Running locally as a consumer:
//
//	go test -tags azure -count=1 ./...

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
	confii "github.com/confiify/confii-go"
)

// Compile-time assertion that fakeAzureCredential implements
// [azcore.TokenCredential]. This is the static half of the G26
// regression guard: prior to the fix the constructor accepted `any`
// and would have compiled regardless of whether the value satisfied
// the SDK's documented credential contract. After the fix the
// constructor's parameter type IS [azcore.TokenCredential], so the
// assertion below + the call sites further down only compile when
// the fake is interface-conformant.
var _ azcore.TokenCredential = (*fakeAzureCredential)(nil)

// fakeAzureCredential is a minimal [azcore.TokenCredential] that returns a
// canned bearer token without ever calling out to AAD. The fake records
// each GetToken invocation so individual tests can assert that the SDK
// actually consulted the credential during the Key Vault REST call.
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

// azureFixture wires a httptest TLS server up as a fake Key Vault endpoint.
type azureFixture struct {
	server *httptest.Server
	routes map[string]func(http.ResponseWriter, *http.Request)
}

func newAzureFixture(t *testing.T) *azureFixture {
	t.Helper()
	f := &azureFixture{routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Key Vault's challenge policy issues a 401 challenge on the
		// first request. The fake responds 401 with a WWW-Authenticate
		// header so the SDK retries with an Authorization header.
		// Subsequent requests hit the registered route.
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

// newAzureKeyVaultForTest builds an [AzureKeyVault] pointed at the fixture's
// TLS endpoint with a transport that trusts the fixture's self-signed cert.
// We construct the underlying `*azsecrets.Client` directly so the test can
// inject the transport — the production [NewAzureKeyVault] does not expose
// transport overrides (and shouldn't, in production). The store struct
// itself is identical to what NewAzureKeyVault would have produced, so this
// still exercises the full GetSecret/SetSecret/etc. code paths.
func newAzureKeyVaultForTest(t *testing.T, vaultURL string, cred azcore.TokenCredential) *AzureKeyVault {
	t.Helper()
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test fixture cert
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

// TestAzure_NewKeyVault_AcceptsTokenCredentialInterface is the G26
// regression guard: prior to the fix, the constructor's `any` parameter
// rejected any concrete type other than `*azidentity.DefaultAzureCredential`
// at runtime via type assertion. After the fix, ANY
// [azcore.TokenCredential] is accepted at compile time. The
// `var _ azcore.TokenCredential = ...` block above asserts the interface
// statically; this test additionally drives the SDK end-to-end with the
// fake credential to prove the bearer token threads through.
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

	// Compile-time check: NewAzureKeyVault accepts the fake credential
	// because its parameter is now [azcore.TokenCredential]. The result
	// is discarded — we use the test-only constructor below to inject
	// the TLS-tolerant transport.
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

// TestAzure_NewKeyVault_NilCredentialUsesDefault asserts that a nil
// credential triggers the implicit DefaultAzureCredential path. We
// can't reach the network here (the default credential will fail to
// resolve in CI), but we CAN observe that the constructor either
// returns the store or a wrapped error mentioning "default credential"
// — in both cases proving the nil-fallback branch was taken.
func TestAzure_NewKeyVault_NilCredentialUsesDefault(t *testing.T) {
	store, err := NewAzureKeyVault("https://example.vault.azure.net/", nil)
	if err != nil {
		// Acceptable: the default credential chain failed in this test
		// environment. The error MUST mention either "default credential"
		// or "azure" to prove we entered the nil-fallback branch and not
		// some unrelated path.
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

// TestAzure_GetSecret_InvalidName surfaces ErrSecretValidation for names
// that violate Azure's naming rules without contacting the network.
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

// TestAzure_GetSecret_NotFound surfaces ErrSecretAccess when the SDK
// reports a 404 from Azure Key Vault. The production code wraps SDK
// errors as ErrSecretAccess.
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
