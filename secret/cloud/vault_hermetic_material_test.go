// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hermetic construction is the security boundary: it decides what TLS material
// is trusted, whether a redirect can move a request to another host, and
// whether a proxy is dialled. Its failure paths matter as much as its happy one
// — a mistake there is silent, and the request still goes out.

func hermetic(t *testing.T, opts ...VaultOption) (*VaultStore, error) {
	t.Helper()
	all := append([]VaultOption{
		WithVaultURL("https://vault.example.invalid:8200"),
		WithVaultHermetic(),
		WithVaultToken("s.test-token"),
	}, opts...)
	return NewVaultWithContext(context.Background(), all...)
}

func TestHermetic_RejectsUnusableTLSMaterial(t *testing.T) {
	t.Run("CA bundle with no certificate", func(t *testing.T) {
		_, err := hermetic(t, WithVaultTLS(VaultTLS{
			CACertPEM: []byte("-----BEGIN CERTIFICATE-----\nnot base64\n-----END CERTIFICATE-----\n"),
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no usable PEM certificate")
	})

	t.Run("client certificate without its key", func(t *testing.T) {
		_, err := hermetic(t, WithVaultTLS(VaultTLS{ClientCertPEM: []byte("cert")}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be supplied together")
	})

	t.Run("client key without its certificate", func(t *testing.T) {
		_, err := hermetic(t, WithVaultTLS(VaultTLS{ClientKeyPEM: []byte("key")}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be supplied together")
	})

	t.Run("mismatched key pair", func(t *testing.T) {
		_, err := hermetic(t, WithVaultTLS(VaultTLS{
			ClientCertPEM: []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"),
			ClientKeyPEM:  []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"),
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "client key pair")
		// The message describes the structure, never the key bytes.
		assert.NotContains(t, err.Error(), "AAAA")
	})
}

func TestHermetic_RejectsAnUndiallableProxy(t *testing.T) {
	_, err := hermetic(t, WithVaultProxy("://not-a-url"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy")
}

// A redirect can move a request to a host the caller never named, which is the
// whole reason hermetic mode refuses them by default.
func TestHermetic_RefusesRedirectsUnlessAsked(t *testing.T) {
	store, err := hermetic(t)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	cfg := store.client.CloneConfig()
	require.NotNil(t, cfg.HttpClient)
	require.NotNil(t, cfg.HttpClient.CheckRedirect,
		"a hermetic client must install a redirect policy")
	assert.Error(t, cfg.HttpClient.CheckRedirect(&http.Request{}, nil),
		"the default policy refuses the redirect")

	following, err := hermetic(t, WithVaultFollowRedirects(true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = following.Close() })
	assert.Nil(t, following.client.CloneConfig().HttpClient.CheckRedirect,
		"following redirects leaves the transport's own policy in place")
}

// Close releases idle connections. It is idempotent and safe on a zero value,
// because the configuration lifecycle calls it without knowing how the store
// was built.
func TestVaultStore_CloseIsSafeAndIdempotent(t *testing.T) {
	store, err := hermetic(t)
	require.NoError(t, err)

	require.NoError(t, store.Close())
	assert.NoError(t, store.Close(), "closing twice must not fail")

	var nilStore *VaultStore
	assert.NoError(t, nilStore.Close(), "a nil store closes cleanly")
	assert.NoError(t, (&VaultStore{}).Close(), "a store with no client closes cleanly")
}
