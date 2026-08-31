// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vaultProvider exercises the declarative mapping and then builds the store it
// describes, so a test can assert on the client the configuration produced.
func vaultProvider(t *testing.T, cfg map[string]any) (*VaultStore, error) {
	t.Helper()
	opts, err := vaultSelfConfigOptions(cfg)
	if err != nil {
		return nil, err
	}
	return NewVaultWithContext(context.Background(), opts...)
}

// Strict mode makes configuration the sole authority. Without it the provider
// falls back to VAULT_ADDR and VAULT_TOKEN, which is convenient for local work
// and wrong for a deployment that means to declare everything.

func TestVaultSelfConfig_StrictRejectsAmbientAddress(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://ambient.example.invalid:8200")

	_, err := vaultProvider(t, map[string]any{"strict": true})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
	assert.Contains(t, err.Error(), "address",
		"the error must name the missing setting")
	assert.NotContains(t, err.Error(), "ambient.example.invalid",
		"the error must not echo the ambient value it refused")
}

// Strict mode requires that VAULT_TOKEN is not consulted. It does not require a
// token to be declared: a provider may authenticate through a configured auth
// method instead, so absence is legitimate and adoption is not.
func TestVaultSelfConfig_StrictDoesNotAdoptAmbientToken(t *testing.T) {
	t.Setenv("VAULT_TOKEN", "s.ambient-token-value")

	store, err := vaultProvider(t, map[string]any{
		"strict":  true,
		"address": "https://vault.example.invalid:8200",
	})
	require.NoError(t, err, "a strict provider without a token is valid; auth may supply one")
	assert.Empty(t, store.client.Token(),
		"VAULT_TOKEN must not be adopted when the configuration declares no token")
}

func TestVaultSelfConfig_StrictAcceptsFullyDeclaredConfiguration(t *testing.T) {
	t.Setenv("VAULT_ADDR", "https://ambient.example.invalid:8200")
	t.Setenv("VAULT_TOKEN", "s.ambient-token-value")
	t.Setenv("VAULT_SKIP_VERIFY", "true")

	store, err := vaultProvider(t, map[string]any{
		"strict":  true,
		"address": "https://declared.example.invalid:8200",
		"token":   "s.declared-token",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://declared.example.invalid:8200", store.client.Address(),
		"the declared address must win over the ambient one")
	assert.Equal(t, "s.declared-token", store.client.Token())
}

// Strict implies hermetic: a configuration claiming to be the sole authority
// must not have its transport shaped by the environment either.
func TestVaultSelfConfig_StrictBuildsHermeticTransport(t *testing.T) {
	t.Setenv("VAULT_SKIP_VERIFY", "true")
	t.Setenv("HTTPS_PROXY", "http://ambient-proxy.example.invalid:3128")

	store, err := vaultProvider(t, map[string]any{
		"strict":  true,
		"address": "https://declared.example.invalid:8200",
		"token":   "s.declared-token",
	})
	require.NoError(t, err)

	transport, ok := store.client.CloneConfig().HttpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, transport.Proxy, "strict mode must not adopt an ambient proxy")
	if transport.TLSClientConfig != nil {
		assert.False(t, transport.TLSClientConfig.InsecureSkipVerify,
			"strict mode must not let an ambient variable disable verification")
	}
}

func TestVaultSelfConfig_NonStrictKeepsAmbientFallback(t *testing.T) {
	// The documented convenience behaviour, pinned so the difference between
	// the two modes stays visible.
	t.Setenv("VAULT_ADDR", "https://ambient.example.invalid:8200")

	store, err := vaultProvider(t, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "https://ambient.example.invalid:8200", store.client.Address())
}

func TestVaultSelfConfig_TransportSettingsAreDeclarative(t *testing.T) {
	store, err := vaultProvider(t, map[string]any{
		"strict":      true,
		"address":     "https://declared.example.invalid:8200",
		"token":       "s.declared-token",
		"timeout":     "7s",
		"retry_limit": 4,
		"proxy":       "http://declared-proxy.example.invalid:8080",
	})
	require.NoError(t, err)

	cfg := store.client.CloneConfig()
	assert.Equal(t, 4, cfg.MaxRetries)
	assert.Equal(t, 7*time.Second, cfg.HttpClient.Timeout)

	transport, ok := cfg.HttpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.Proxy, "a declared proxy must be used")
	req, err := http.NewRequest(http.MethodGet, "https://declared.example.invalid:8200/v1/x", nil)
	require.NoError(t, err)
	proxyURL, err := transport.Proxy(req)
	require.NoError(t, err)
	require.NotNil(t, proxyURL)
	assert.Equal(t, "http://declared-proxy.example.invalid:8080", proxyURL.String())
}

func TestVaultSelfConfig_TLSMaterialIsDeclarative(t *testing.T) {
	_, err := vaultProvider(t, map[string]any{
		"strict":  true,
		"address": "https://declared.example.invalid:8200",
		"token":   "s.declared-token",
		"tls": map[string]any{
			"ca_cert_pem": "not-a-certificate",
		},
	})
	require.Error(t, err, "malformed declared TLS material must be a hard error")
	assert.NotContains(t, err.Error(), "not-a-certificate",
		"the error must not echo supplied material")
}

func TestVaultSelfConfig_RejectsUnknownStrictSetting(t *testing.T) {
	_, err := vaultProvider(t, map[string]any{
		"strict":     true,
		"address":    "https://declared.example.invalid:8200",
		"token":      "s.declared-token",
		"typo_field": "value",
	})
	require.Error(t, err, "strict mode must reject settings it does not understand")
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
	assert.Contains(t, err.Error(), "typo_field",
		"the error must name the offending key")
}

// A VaultStore holds an HTTP client with pooled connections. Config.Close
// already closes resources implementing Close() error, so implementing it is
// how the provider joins configuration shutdown.
func TestVaultStore_CloseReleasesIdleConnections(t *testing.T) {
	store, err := NewVaultWithContext(context.Background(),
		WithVaultHermetic(),
		WithVaultURL("https://vault.example.invalid:8200"),
		WithVaultToken("s.token"),
	)
	require.NoError(t, err)

	require.NoError(t, store.Close())
	assert.NoError(t, store.Close(), "close must be idempotent")
}

func TestVaultStore_SatisfiesCloser(t *testing.T) {
	store, err := NewVaultWithContext(context.Background(),
		WithVaultHermetic(),
		WithVaultURL("https://vault.example.invalid:8200"),
	)
	require.NoError(t, err)

	var closer any = store
	_, ok := closer.(interface{ Close() error })
	assert.True(t, ok, "VaultStore must satisfy the optional closer contract")
}

// A schema closed only at its root is not closed. An unrecognised key inside
// tls or auth was accepted, so a mistyped nested setting kept its default
// silently — the exact failure strict mode exists to prevent, one level down.
func TestVaultSelfConfig_StrictIsRecursivelyClosed(t *testing.T) {
	base := func(extra map[string]any) map[string]any {
		cfg := map[string]any{
			"strict":  true,
			"address": "https://vault.example.invalid:8200",
			"token":   "s.tok",
		}
		for k, v := range extra {
			cfg[k] = v
		}
		return cfg
	}

	for name, cfg := range map[string]map[string]any{
		"tls":  base(map[string]any{"tls": map[string]any{"typo": "x"}}),
		"auth": base(map[string]any{"auth": map[string]any{"method": "token", "token": "x", "typo": "x"}}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vaultProvider(t, cfg)
			require.Error(t, err, "an unrecognized %s setting must be rejected", name)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
			assert.Contains(t, err.Error(), "typo")
		})
	}
}

func TestVaultSelfConfig_StrictRejectsUnsupportedAuthMethod(t *testing.T) {
	_, err := vaultProvider(t, map[string]any{
		"strict":  true,
		"address": "https://vault.example.invalid:8200",
		"auth":    map[string]any{"method": "no-such-method"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
}

// Accepting two spellings of one setting means one is silently ignored, and
// which one is an implementation detail the author cannot see.
func TestVaultSelfConfig_StrictRejectsConflictingAliases(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"address vs url": {
			"strict": true, "token": "s.tok",
			"address": "https://a.example.invalid:8200",
			"url":     "https://b.example.invalid:8200",
		},
		"mount vs mount_point": {
			"strict": true, "token": "s.tok",
			"address": "https://a.example.invalid:8200",
			"mount":   "one", "mount_point": "two",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vaultProvider(t, cfg)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		})
	}
}

func TestVaultSelfConfig_RejectsUnusableTimeout(t *testing.T) {
	for _, raw := range []string{"-1s", "0s"} {
		t.Run(raw, func(t *testing.T) {
			_, err := vaultProvider(t, map[string]any{
				"strict": true, "address": "https://v.example.invalid:8200",
				"token": "s.tok", "timeout": raw,
			})
			require.Error(t, err, "%s is not a usable timeout", raw)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		})
	}
}

func TestVaultSelfConfig_RejectsUnusableProxy(t *testing.T) {
	for name, proxy := range map[string]string{
		"relative":           "no-scheme-host",
		"scheme only":        "http://",
		"unsupported scheme": "ftp://proxy.example.invalid:21",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vaultProvider(t, map[string]any{
				"strict": true, "address": "https://v.example.invalid:8200",
				"token": "s.tok", "proxy": proxy,
			})
			require.Error(t, err, "%s must be rejected", name)
			assert.ErrorIs(t, err, ErrVaultStrictConfiguration)
		})
	}
}

// A proxy URL may carry credentials in its user information; a parse failure
// must not put them in a log.
func TestVaultSelfConfig_ProxyErrorDoesNotLeakCredentials(t *testing.T) {
	_, err := vaultProvider(t, map[string]any{
		"strict": true, "address": "https://v.example.invalid:8200",
		"token": "s.tok", "proxy": "ftp://user:sup3rsecret@proxy.example.invalid:21",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "sup3rsecret",
		"proxy credentials must never appear in an error")
}

// int(value) on a float truncates, so 1.5 became 1: a configuration that says
// 1.5 does not mean 1.
func TestVaultSelfConfig_RejectsFractionalInteger(t *testing.T) {
	_, err := vaultProvider(t, map[string]any{
		"strict": true, "address": "https://v.example.invalid:8200",
		"token": "s.tok", "retry_limit": 1.5,
	})
	require.Error(t, err, "a fractional retry limit must not be silently truncated")
	assert.Contains(t, err.Error(), "whole number")
}

func TestVaultSelfConfig_AcceptsWholeNumberFloat(t *testing.T) {
	store, err := vaultProvider(t, map[string]any{
		"strict": true, "address": "https://v.example.invalid:8200",
		"token": "s.tok", "retry_limit": float64(3),
	})
	require.NoError(t, err, "a whole number expressed as a float is still whole")
	assert.Equal(t, 3, store.client.CloneConfig().MaxRetries)
}
