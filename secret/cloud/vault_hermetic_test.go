// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostileVaultEnvironment sets every Vault and proxy variable the SDK consults
// to a value that conflicts with, or is malformed relative to, what the caller
// passes explicitly. A hermetic store must be identical whether or not this
// ran.
func hostileVaultEnvironment(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"VAULT_ADDR":              "https://ambient.example.invalid:8200",
		"VAULT_AGENT_ADDR":        "https://ambient-agent.example.invalid:8200",
		"VAULT_CLIENT_TIMEOUT":    "1",
		"VAULT_HEADERS":           `{"X-Ambient-Header":"leaked"}`,
		"VAULT_NAMESPACE":         "ambient-namespace",
		"VAULT_MAX_RETRIES":       "42",
		"VAULT_PROXY_ADDR":        "http://ambient-vault-proxy.example.invalid:3128",
		"VAULT_HTTP_PROXY":        "http://ambient-vault-http-proxy.example.invalid:3128",
		"VAULT_SKIP_VERIFY":       "true",
		"VAULT_SRV_LOOKUP":        "true",
		"VAULT_TLS_SERVER_NAME":   "ambient.example.invalid",
		"VAULT_TOKEN":             "ambient-token-must-not-be-used",
		"VAULT_DISABLE_REDIRECTS": "true",
		"HTTP_PROXY":              "http://ambient-proxy.example.invalid:3128",
		"HTTPS_PROXY":             "http://ambient-proxy.example.invalid:3128",
		"NO_PROXY":                "ambient.example.invalid",
	} {
		t.Setenv(name, value)
	}
}

func newHermeticTestStore(t *testing.T, extra ...VaultOption) *VaultStore {
	t.Helper()
	opts := append([]VaultOption{
		WithVaultHermetic(),
		WithVaultURL("https://explicit.example.invalid:8200"),
		WithVaultToken("explicit-token"),
		WithVaultNamespace("explicit-namespace"),
		WithVaultTimeout(9 * time.Second),
		WithVaultRetryLimit(3),
	}, extra...)
	store, err := NewVaultWithContext(context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, store)
	return store
}

func hermeticTransport(t *testing.T, store *VaultStore) *http.Transport {
	t.Helper()
	client := store.client.CloneConfig().HttpClient
	require.NotNil(t, client)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "hermetic store must own an *http.Transport, got %T", client.Transport)
	return transport
}

func TestVaultHermetic_TLSVerificationSurvivesAmbientSkipVerify(t *testing.T) {
	hostileVaultEnvironment(t)
	transport := hermeticTransport(t, newHermeticTestStore(t))

	if transport.TLSClientConfig != nil {
		assert.False(t, transport.TLSClientConfig.InsecureSkipVerify,
			"ambient VAULT_SKIP_VERIFY must never disable certificate verification")
		assert.NotEqual(t, "ambient.example.invalid", transport.TLSClientConfig.ServerName,
			"ambient VAULT_TLS_SERVER_NAME must not be adopted")
	}
}

func TestVaultHermetic_NoAmbientProxyIsSelected(t *testing.T) {
	hostileVaultEnvironment(t)
	transport := hermeticTransport(t, newHermeticTestStore(t))

	require.Nil(t, transport.Proxy,
		"hermetic transport must not consult any proxy resolver by default")
}

func TestVaultHermetic_ExplicitProxyIsHonored(t *testing.T) {
	hostileVaultEnvironment(t)
	store := newHermeticTestStore(t, WithVaultProxy("http://explicit-proxy.example.invalid:8080"))
	transport := hermeticTransport(t, store)

	require.NotNil(t, transport.Proxy)
	req, err := http.NewRequest(http.MethodGet, "https://explicit.example.invalid:8200/v1/x", nil)
	require.NoError(t, err)
	proxyURL, err := transport.Proxy(req)
	require.NoError(t, err)
	require.NotNil(t, proxyURL)
	assert.Equal(t, "http://explicit-proxy.example.invalid:8080", proxyURL.String(),
		"only the explicitly configured proxy may be used")
}

func TestVaultHermetic_AmbientNamespaceAndTokenAreNotAdopted(t *testing.T) {
	hostileVaultEnvironment(t)
	store := newHermeticTestStore(t)

	assert.Equal(t, "explicit-namespace", store.client.Namespace(),
		"ambient VAULT_NAMESPACE must not override the explicit namespace")
	assert.Equal(t, "explicit-token", store.client.Token(),
		"ambient VAULT_TOKEN must not override the explicit token")
	assert.Empty(t, store.client.Headers().Get("X-Ambient-Header"),
		"ambient VAULT_HEADERS must not be adopted")
}

func TestVaultHermetic_ExplicitAddressTimeoutAndRetriesArePreserved(t *testing.T) {
	hostileVaultEnvironment(t)
	store := newHermeticTestStore(t)
	cfg := store.client.CloneConfig()

	assert.Equal(t, "https://explicit.example.invalid:8200", store.client.Address())
	assert.Equal(t, 3, cfg.MaxRetries, "ambient VAULT_MAX_RETRIES must not win")
	assert.Equal(t, 9*time.Second, cfg.HttpClient.Timeout,
		"ambient VAULT_CLIENT_TIMEOUT must not win")
	assert.False(t, cfg.SRVLookup, "ambient VAULT_SRV_LOOKUP must not be adopted")
}

func TestVaultHermetic_OwnsItsClientAndTransport(t *testing.T) {
	hostileVaultEnvironment(t)
	store := newHermeticTestStore(t)
	client := store.client.CloneConfig().HttpClient

	assert.NotSame(t, http.DefaultClient, client,
		"the provider must own its http.Client, never share http.DefaultClient")
	assert.NotSame(t, http.DefaultTransport, client.Transport,
		"the provider must own its transport, never share http.DefaultTransport")

	// Sharing the default transport would let one store's TLS settings leak
	// into every other user of the process-wide transport.
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	assert.NotNil(t, defaultTransport.Proxy,
		"sanity: http.DefaultTransport still resolves proxies from the environment, "+
			"which is exactly what the hermetic transport must avoid")
}

func TestVaultHermetic_DoesNotMutateProcessEnvironment(t *testing.T) {
	hostileVaultEnvironment(t)

	before := os.Environ()
	_ = newHermeticTestStore(t)
	after := os.Environ()

	assert.ElementsMatch(t, before, after,
		"construction must not add, remove, or alter any environment variable")
}

// Unreadable ambient TLS material is the one ambient condition a hermetic
// client cannot shrug off. api.NewClient builds api.DefaultConfig internally
// before reading the config it is handed, so the SDK parses the environment
// whatever we pass. Clearing the variable around the call would mutate
// process-global state shared with every other goroutine, so the limitation is
// reported precisely rather than worked around. This test pins that contract:
// a named, typed error — never a silent fallback to ambient TLS settings.
func TestVaultHermetic_MalformedAmbientTLSFailsWithNamedError(t *testing.T) {
	for _, name := range []string{"VAULT_CACERT", "VAULT_CLIENT_CERT", "VAULT_CAPATH"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "/nonexistent/ambient-material.pem")

			_, err := NewVaultWithContext(context.Background(),
				WithVaultHermetic(),
				WithVaultURL("https://explicit.example.invalid:8200"),
				WithVaultToken("explicit-token"),
			)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrVaultAmbientEnvironment,
				"the failure must be attributable to the ambient environment, not silently absorbed")
		})
	}
}

func TestVaultHermetic_MalformedAmbientScalarFailsWithNamedError(t *testing.T) {
	t.Setenv("VAULT_MAX_RETRIES", "not-a-number")

	_, err := NewVaultWithContext(context.Background(),
		WithVaultHermetic(),
		WithVaultURL("https://explicit.example.invalid:8200"),
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrVaultAmbientEnvironment)
}

func TestVaultHermetic_RejectsMalformedExplicitCA(t *testing.T) {
	_, err := NewVaultWithContext(context.Background(),
		WithVaultHermetic(),
		WithVaultURL("https://explicit.example.invalid:8200"),
		WithVaultTLS(VaultTLS{CACertPEM: []byte("not-a-certificate")}),
	)
	require.Error(t, err, "an explicitly supplied malformed CA must be a hard error")
	assert.NotContains(t, err.Error(), "not-a-certificate",
		"errors must not echo supplied key or certificate material")
}

func TestVaultHermetic_ErrorsDoNotLeakCredentials(t *testing.T) {
	const token = "s.supersecrettokenvalue"
	_, err := NewVaultWithContext(context.Background(),
		WithVaultHermetic(),
		WithVaultURL("://malformed-address"),
		WithVaultToken(token),
	)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token, "errors must never contain the token")
}

func TestVaultHermetic_NonHermeticPathStillInheritsEnvironment(t *testing.T) {
	// The documented legacy mode is retained deliberately. This test pins that
	// contract so the difference between the two modes stays visible.
	hostileVaultEnvironment(t)
	store, err := NewVaultWithContext(context.Background(),
		WithVaultURL("https://explicit.example.invalid:8200"),
		WithVaultToken("explicit-token"),
	)
	require.NoError(t, err)
	assert.Equal(t, "ambient-namespace", store.client.Namespace(),
		"legacy mode still inherits the ambient namespace; use WithVaultHermetic to opt out")
}

// vaultConfig is unexported but reachable through the exported VaultOption
// signature, so apidiff tracks its comparability as public API. Holding
// VaultTLS by value here would embed []byte fields and break it.
func TestVaultConfig_RemainsComparable(t *testing.T) {
	var a, b vaultConfig
	assert.True(t, a == b, "vaultConfig must stay comparable; hold byte-bearing fields by pointer")
}
