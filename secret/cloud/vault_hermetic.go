// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/vault/api"
)

// VaultTLS carries explicit TLS material for a hermetic Vault client. Every
// field is supplied by the caller as bytes or a literal name; nothing is read
// from a file path found in the environment.
//
// The zero value verifies the server certificate against the host trust store,
// which is the intended configuration for most deployments.
type VaultTLS struct {
	// CACertPEM is a PEM bundle trusted for the server certificate. When empty
	// the host trust store is used.
	CACertPEM []byte
	// ClientCertPEM and ClientKeyPEM enable mutual TLS. Supply both or neither.
	ClientCertPEM []byte
	ClientKeyPEM  []byte
	// ServerName overrides the name checked against the server certificate.
	// Leave empty to check against the address host.
	ServerName string
}

// WithVaultHermetic builds the client only from options supplied by the
// caller. It is the recommended mode for security-sensitive deployments.
//
// The Vault SDK's usual constructor reads roughly twenty environment
// variables, including VAULT_SKIP_VERIFY, which silently disables certificate
// verification, and the standard proxy variables. Under this option none of
// them is consulted: address, namespace, token, headers, TLS material, proxy,
// timeout, retry limit, and redirect policy come from options alone.
//
// The store owns its http.Client and http.Transport and never shares or
// mutates http.DefaultTransport. Proxying is disabled unless
// [WithVaultProxy] is supplied. Certificate verification is always enabled and
// cannot be turned off in this mode; [WithVaultVerify] is ignored, so an
// ambient variable cannot weaken transport security.
//
// The process environment is read for none of these settings and is never
// modified. Values present in the environment are left exactly as found.
//
// For defence in depth, start the process with those variables unset. A
// hermetic client ignores their values either way, but an unset environment
// also removes the one remaining failure described on
// [ErrVaultAmbientEnvironment], where a malformed ambient value fails
// construction because the SDK parses the environment before reading the
// configuration it is handed. See the environment-hygiene section of
// docs/secrets.md.
func WithVaultHermetic() VaultOption {
	return func(c *vaultConfig) { c.Hermetic = true }
}

// WithVaultTLS supplies explicit TLS material. It is honored only in hermetic
// mode, where no certificate or key is ever discovered from the environment.
func WithVaultTLS(t VaultTLS) VaultOption {
	// Held behind a pointer so vaultConfig stays comparable: VaultTLS
	// carries []byte fields, and vaultConfig is reachable through the
	// exported VaultOption signature.
	return func(c *vaultConfig) { c.TLS = &t }
}

// WithVaultProxy routes requests through proxyURL. Without it a hermetic
// client makes direct connections; ambient HTTP_PROXY, HTTPS_PROXY, NO_PROXY,
// VAULT_PROXY_ADDR, and VAULT_HTTP_PROXY are never consulted.
func WithVaultProxy(proxyURL string) VaultOption {
	return func(c *vaultConfig) { c.ProxyURL = proxyURL }
}

// WithVaultTimeout bounds each HTTP request. Zero leaves the SDK default.
func WithVaultTimeout(d time.Duration) VaultOption {
	return func(c *vaultConfig) { c.Timeout = d }
}

// WithVaultRetryLimit caps SDK retries for a request. Zero leaves the SDK
// default; a negative value is rejected at construction.
func WithVaultRetryLimit(n int) VaultOption {
	return func(c *vaultConfig) { c.RetryLimit = n; c.RetryLimitSet = true }
}

// WithVaultFollowRedirects allows the client to follow server redirects. The
// hermetic default refuses them, so a redirect cannot move a request to an
// unintended host.
func WithVaultFollowRedirects(v bool) VaultOption {
	return func(c *vaultConfig) { c.FollowRedirects = v }
}

// Hermetic transport defaults. They match the Vault SDK's documented defaults,
// which api.NewClient does not apply to a caller-supplied api.Config.
const (
	hermeticDefaultTimeout      = 60 * time.Second
	hermeticDefaultRetries      = 2
	hermeticDefaultMinRetryWait = 1000 * time.Millisecond
	hermeticDefaultMaxRetryWait = 1500 * time.Millisecond
)

// errRedirectRefused reports a redirect declined by the hermetic policy. It
// names no credential and carries no response body.
var errRedirectRefused = errors.New("vault: redirect refused by hermetic client")

// ErrVaultAmbientEnvironment reports that a hermetic client could not be built
// because an ambient Vault variable is malformed.
//
// A hermetic client never adopts a value from the environment, but it cannot
// prevent the Vault SDK from parsing that environment: api.NewClient builds
// api.DefaultConfig internally, before reading the configuration it is given,
// and surfaces any parse failure. An unparseable VAULT_MAX_RETRIES,
// VAULT_CLIENT_TIMEOUT, VAULT_SKIP_VERIFY, VAULT_SRV_LOOKUP or
// VAULT_DISABLE_REDIRECTS, or an unreadable VAULT_CACERT, VAULT_CAPATH,
// VAULT_CACERT_BYTES, VAULT_CLIENT_CERT or VAULT_CLIENT_KEY, therefore fails
// construction with this error.
//
// Clearing the variable for the duration of the call would mutate
// process-global state shared by every goroutine, so the condition is reported
// rather than worked around. Correct the ambient value, or unset it, in the
// environment that launches the process. Starting with these variables unset
// is the recommended deployment and removes this failure entirely.
var ErrVaultAmbientEnvironment = errors.New("vault: malformed ambient environment prevents hermetic construction")

// newHermeticVaultClient builds a client whose behavior derives only from cfg.
//
// api.DefaultConfig, which the ambient constructor uses, calls ReadEnvironment
// and adopts roughly twenty variables. This path constructs api.Config
// directly instead. api.NewClient still consults VAULT_TOKEN, VAULT_NAMESPACE,
// and VAULT_HEADERS regardless of the config it is handed, so those are reset
// from options immediately afterwards.
func newHermeticVaultClient(cfg *vaultConfig) (*api.Client, error) {
	tlsConfig, err := hermeticTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		// Nil disables proxying entirely. http.ProxyFromEnvironment, the
		// default elsewhere, would consult the ambient proxy variables.
		Proxy:                 nil,
		TLSClientConfig:       tlsConfig,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if cfg.ProxyURL != "" {
		parsed, perr := url.Parse(cfg.ProxyURL)
		if perr != nil {
			return nil, fmt.Errorf("vault proxy address: %w", perr)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}

	httpClient := &http.Client{Transport: transport}
	if cfg.Timeout > 0 {
		httpClient.Timeout = cfg.Timeout
	}
	if !cfg.FollowRedirects {
		httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	// api.NewClient copies neither Timeout nor MaxRetries from its internal
	// defaults, so a literal api.Config leaves them at zero — no timeout and no
	// retries. Set them explicitly. The values match the SDK's documented
	// defaults so hermetic mode is not quietly less resilient than ambient
	// mode; MinRetryWait and MaxRetryWait are stated for the same reason even
	// though NewClient does copy those.
	apiCfg := &api.Config{
		Address:      cfg.URL,
		HttpClient:   httpClient,
		Timeout:      hermeticDefaultTimeout,
		MaxRetries:   hermeticDefaultRetries,
		MinRetryWait: hermeticDefaultMinRetryWait,
		MaxRetryWait: hermeticDefaultMaxRetryWait,
	}
	if cfg.Timeout > 0 {
		apiCfg.Timeout = cfg.Timeout
	}
	if cfg.RetryLimitSet {
		apiCfg.MaxRetries = cfg.RetryLimit
	}

	client, err := api.NewClient(apiCfg)
	if err != nil {
		// api.NewClient builds api.DefaultConfig internally before it reads
		// the config it was handed, and returns that default's error. A
		// malformed ambient value therefore fails construction even though
		// nothing from the environment is used. The environment cannot be
		// cleared to avoid this without mutating process-global state, which
		// would race every other goroutine, so the condition is reported
		// precisely instead. See ErrVaultAmbientEnvironment.
		if strings.Contains(err.Error(), "setting up default configuration") {
			return nil, fmt.Errorf("%w: %w", ErrVaultAmbientEnvironment, err)
		}
		// The SDK error can quote the address but never the token, which is
		// applied below. Wrap without adding caller material.
		return nil, fmt.Errorf("vault client: %w", err)
	}

	// api.NewClient adopts VAULT_TOKEN, VAULT_NAMESPACE, and VAULT_HEADERS
	// even from an explicit config. Overwrite all three unconditionally so an
	// ambient value can never survive into a hermetic client.
	//
	// Clearing the headers also removes X-Vault-Request, which api.NewClient
	// installs as SSRF protection and which Vault Agent relies on. Dropping it
	// would make a hermetic client less safe than an ambient one, so it is
	// restored rather than left to the caller.
	client.ClearToken()
	client.ClearNamespace()
	client.SetHeaders(http.Header{
		api.RequestHeaderName: []string{"true"},
	})

	// A nil wrapping lookup makes the SDK fall back to
	// api.DefaultWrappingLookupFunc, which reads VAULT_WRAP_TTL on every
	// request. That is ambient state reaching a hermetic client at request
	// time rather than construction time, so the lookup is set explicitly to
	// request no wrapping.
	client.SetWrappingLookupFunc(func(string, string) string { return "" })
	if cfg.Namespace != "" {
		client.SetNamespace(cfg.Namespace)
	}
	if !cfg.FollowRedirects {
		client.SetCheckRedirect(func(*http.Request, []*http.Request) error {
			return errRedirectRefused
		})
	}
	return client, nil
}

// hermeticTLSConfig builds the TLS configuration from explicit material only.
// Verification is always on: the hermetic contract does not offer a way to
// disable it, so no option and no environment variable can.
func hermeticTLSConfig(t *VaultTLS) (*tls.Config, error) {
	if t == nil {
		t = &VaultTLS{}
	}
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: false,
		ServerName:         t.ServerName,
	}

	if len(t.CACertPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(t.CACertPEM) {
			// Deliberately does not echo the supplied bytes.
			return nil, errors.New("vault TLS: CA bundle contains no usable PEM certificate")
		}
		cfg.RootCAs = pool
	}

	switch {
	case len(t.ClientCertPEM) > 0 && len(t.ClientKeyPEM) > 0:
		pair, err := tls.X509KeyPair(t.ClientCertPEM, t.ClientKeyPEM)
		if err != nil {
			// tls errors describe the structure, never the key bytes.
			return nil, fmt.Errorf("vault TLS: client key pair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	case len(t.ClientCertPEM) > 0 || len(t.ClientKeyPEM) > 0:
		return nil, errors.New("vault TLS: client certificate and key must be supplied together")
	}

	return cfg, nil
}

// Close releases the resources the store owns. It shuts idle HTTP connections
// rather than waiting for them to time out, and is idempotent and safe to call
// concurrently.
//
// [confii.Config] closes resources implementing this method during shutdown, so
// a store reached through declarative configuration is closed with the
// configuration. A store constructed directly is the caller's to close.
//
// Close does not invalidate the Vault token: a token's lifetime is Vault's to
// manage through its lease, and revoking one here would break any other client
// sharing it.
func (s *VaultStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	if httpClient := s.client.CloneConfig().HttpClient; httpClient != nil {
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
	return nil
}
