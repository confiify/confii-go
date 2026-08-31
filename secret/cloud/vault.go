// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/hashicorp/vault/api"
)

// VaultStore implements SecretStore for Vault-compatible servers, including
// HashiCorp Vault and OpenBao.
type VaultStore struct {
	client     *api.Client
	mountPoint string
	kvVersion  int
	namespace  string
}

// VaultOption configures a VaultStore.
type VaultOption func(*vaultConfig)

type vaultConfig struct {
	URL        string
	Token      string
	RoleID     string
	SecretID   string
	AuthMethod VaultAuthMethod
	Namespace  string
	MountPoint string
	KVVersion  int
	Verify     bool

	// Hermetic and the fields below configure the environment-independent
	// client built by newHermeticVaultClient. See WithVaultHermetic.
	Hermetic        bool
	TLS             *VaultTLS
	ProxyURL        string
	Timeout         time.Duration
	RetryLimit      int
	RetryLimitSet   bool
	FollowRedirects bool
}

// VaultAuthMethod exchanges provider-specific credentials for a Vault client
// token. Implementations must honor ctx and return an empty token only with an
// error. Authenticate may modify client only as required by its login flow.
type VaultAuthMethod interface {
	// Authenticate returns a non-empty Vault client token.
	Authenticate(context.Context, *api.Client) (string, error)
}

// WithVaultURL sets the Vault-compatible server address. The default is
// http://127.0.0.1:8200. Production deployments should use HTTPS.
func WithVaultURL(url string) VaultOption { return func(c *vaultConfig) { c.URL = url } }

// WithVaultToken configures a static client token. WithVaultAuth takes
// precedence when both are supplied.
func WithVaultToken(token string) VaultOption { return func(c *vaultConfig) { c.Token = token } }

// WithVaultNamespace sets the Vault Enterprise or OpenBao namespace header.
func WithVaultNamespace(ns string) VaultOption { return func(c *vaultConfig) { c.Namespace = ns } }

// WithVaultMountPoint sets the KV secrets-engine mount name. The default is
// "secret"; pass the mount name only, without data or metadata path segments.
func WithVaultMountPoint(mp string) VaultOption { return func(c *vaultConfig) { c.MountPoint = mp } }

// WithVaultKVVersion selects KV engine version 1 or 2. The default is 2;
// construction rejects every other value.
func WithVaultKVVersion(v int) VaultOption { return func(c *vaultConfig) { c.KVVersion = v } }

// WithVaultVerify controls TLS certificate verification. Verification is
// enabled by default. Disabling it is intended only for controlled development
// environments and permits man-in-the-middle attacks.
func WithVaultVerify(v bool) VaultOption { return func(c *vaultConfig) { c.Verify = v } }

// WithVaultAuth selects a token-producing authentication method. It takes
// precedence over a static token and WithVaultAppRole credentials.
func WithVaultAuth(am VaultAuthMethod) VaultOption { return func(c *vaultConfig) { c.AuthMethod = am } }

// WithVaultAppRole configures AppRole credentials at the default
// auth/approle/login mount. It is used only when no WithVaultAuth method or
// static token is configured. For a custom AppRole mount, use WithVaultAuth
// with AppRoleAuth.
func WithVaultAppRole(roleID, secretID string) VaultOption {
	return func(c *vaultConfig) {
		c.RoleID = roleID
		c.SecretID = secretID
	}
}

// NewHashiCorpVault creates a HashiCorp Vault store using a 60-second implicit
// startup timeout. Construction configures the client and performs login when
// an authentication method or AppRole is supplied; it does not verify the KV
// mount with a read. Use NewHashiCorpVaultWithContext to control startup
// cancellation explicitly.
func NewHashiCorpVault(opts ...VaultOption) (*VaultStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return NewHashiCorpVaultWithContext(ctx, opts...)
}

// NewHashiCorpVaultWithContext creates a Vault secret store and binds any
// authentication network calls to ctx. Callers that need a startup deadline
// should pass one here; the returned store uses each operation's context for
// subsequent reads and writes.
func NewHashiCorpVaultWithContext(ctx context.Context, opts ...VaultOption) (*VaultStore, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", confii.ErrVaultAuth)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := &vaultConfig{
		URL:        "http://127.0.0.1:8200",
		MountPoint: "secret",
		KVVersion:  2,
		Verify:     true,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.KVVersion != 1 && cfg.KVVersion != 2 {
		return nil, fmt.Errorf("vault KV version must be 1 or 2, got %d", cfg.KVVersion)
	}

	if cfg.RetryLimitSet && cfg.RetryLimit < 0 {
		return nil, fmt.Errorf("vault retry limit must not be negative, got %d", cfg.RetryLimit)
	}

	var client *api.Client
	if cfg.Hermetic {
		// Behavior derives only from the options above; see WithVaultHermetic.
		hermetic, err := newHermeticVaultClient(cfg)
		if err != nil {
			return nil, err
		}
		client = hermetic
	} else {
		// Ambient mode: api.DefaultConfig reads the VAULT_* and proxy
		// environment. Retained for compatibility and documented as such.
		vaultCfg := api.DefaultConfig()
		vaultCfg.Address = cfg.URL
		if !cfg.Verify {
			if err := vaultCfg.ConfigureTLS(&api.TLSConfig{Insecure: true}); err != nil {
				return nil, fmt.Errorf("vault TLS config: %w", err)
			}
		}

		ambient, err := api.NewClient(vaultCfg)
		if err != nil {
			return nil, fmt.Errorf("vault client: %w", err)
		}
		client = ambient

		if cfg.Namespace != "" {
			client.SetNamespace(cfg.Namespace)
		}
	}

	// Authenticate: auth_method > token > role_id+secret_id
	switch {
	case cfg.AuthMethod != nil:
		token, err := cfg.AuthMethod.Authenticate(ctx, client)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", confii.ErrVaultAuth, err)
		}
		if token == "" {
			return nil, fmt.Errorf("%w: auth method returned an empty token", confii.ErrVaultAuth)
		}
		client.SetToken(token)
	case cfg.Token != "":
		client.SetToken(cfg.Token)
	case cfg.RoleID != "" && cfg.SecretID != "":
		secret, err := client.Logical().WriteWithContext(ctx, "auth/approle/login", map[string]any{
			"role_id":   cfg.RoleID,
			"secret_id": cfg.SecretID,
		})
		if err != nil {
			return nil, fmt.Errorf("%w: approle login: %w", confii.ErrVaultAuth, err)
		}
		if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
			return nil, fmt.Errorf("%w: approle login returned no auth token", confii.ErrVaultAuth)
		}
		client.SetToken(secret.Auth.ClientToken)
	}

	return &VaultStore{
		client:     client,
		mountPoint: cfg.MountPoint,
		kvVersion:  cfg.KVVersion,
		namespace:  cfg.Namespace,
	}, nil
}

// NewVault is the vendor-neutral alias for [NewHashiCorpVault] and supports
// HashiCorp Vault and OpenBao-compatible servers.
func NewVault(opts ...VaultOption) (*VaultStore, error) {
	return NewHashiCorpVault(opts...)
}

// NewVaultWithContext is the context-aware form of [NewVault].
func NewVaultWithContext(ctx context.Context, opts ...VaultOption) (*VaultStore, error) {
	return NewHashiCorpVaultWithContext(ctx, opts...)
}

// NewOpenBao creates a Vault-compatible secret store for an OpenBao server.
// It supports the same options, authentication methods, and KV operations as
// [NewVault].
func NewOpenBao(opts ...VaultOption) (*VaultStore, error) {
	return NewHashiCorpVault(opts...)
}

// NewOpenBaoWithContext is the context-aware form of [NewOpenBao].
func NewOpenBaoWithContext(ctx context.Context, opts ...VaultOption) (*VaultStore, error) {
	return NewHashiCorpVaultWithContext(ctx, opts...)
}

// GetSecret retrieves key from the configured KV mount. KV v2 accepts
// [confii.WithVersion]; KV v1 has no historical version support, so a
// non-empty version wraps [confii.ErrSecretValidation] rather than silently
// serving the current value. [confii.WithField] returns one top-level field
// from the secret map. Missing keys wrap [confii.ErrSecretNotFound], missing
// fields wrap [confii.ErrSecretValidation], and transport or decoding failures
// wrap [confii.ErrSecretAccess].
func (s *VaultStore) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	o := confii.ResolveSecretOptions(opts...)
	if s.kvVersion != 2 && o.Version != "" {
		return nil, fmt.Errorf(
			"%w: KV v1 mount %q does not support secret versions (requested version %q for %s)",
			confii.ErrSecretValidation, s.mountPoint, o.Version, key,
		)
	}

	var secretPath string
	if s.kvVersion == 2 {
		secretPath = fmt.Sprintf("%s/data/%s", s.mountPoint, key)
	} else {
		secretPath = fmt.Sprintf("%s/%s", s.mountPoint, key)
	}

	// Read the raw response so HTTP 404 is mapped before the Vault SDK tries
	// to decode the body. Logical.ReadWithContext attempts to parse 404 bodies
	// first; a proxy or development server returning a non-Vault body (for
	// example, plain-text "404 page not found") turns a genuine miss into a
	// JSON decode error and loses the status distinction.
	var query map[string][]string
	if s.kvVersion == 2 && o.Version != "" {
		query = map[string][]string{
			"version": {o.Version},
		}
	}
	resp, err := s.client.Logical().ReadRawWithDataWithContext(ctx, secretPath, query)
	if resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", confii.ErrSecretAccess, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("%w: vault returned no response", confii.ErrSecretAccess)
	}
	secret, err := api.ParseSecret(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: decode vault response: %w", confii.ErrSecretAccess, err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
	}

	data := secret.Data
	if s.kvVersion == 2 {
		if d, ok := data["data"].(map[string]any); ok {
			data = d
		}
	}

	// Extract specific field if requested.
	if o.Field != "" {
		v, ok := data[o.Field]
		if !ok {
			return nil, fmt.Errorf("%w: field %q not found in secret %s", confii.ErrSecretValidation, o.Field, key)
		}
		return v, nil
	}

	return data, nil
}

// SetSecret writes key to the configured Vault or OpenBao KV mount. Map values
// are stored as fields; every other value is stored under the field "value".
// Secret options are currently ignored. The operation honors ctx and returns
// the backend error unchanged.
func (s *VaultStore) SetSecret(ctx context.Context, key string, value any, _ ...confii.SecretOption) error {
	var data map[string]any
	switch v := value.(type) {
	case map[string]any:
		data = v
	default:
		data = map[string]any{"value": v}
	}

	var secretPath string
	var writeData map[string]any
	if s.kvVersion == 2 {
		secretPath = fmt.Sprintf("%s/data/%s", s.mountPoint, key)
		writeData = map[string]any{"data": data}
	} else {
		secretPath = fmt.Sprintf("%s/%s", s.mountPoint, key)
		writeData = data
	}

	_, err := s.client.Logical().WriteWithContext(ctx, secretPath, writeData)
	return err
}

// DeleteSecret removes key from the configured KV mount. For KV v2 it deletes
// metadata and all versions; for KV v1 it deletes the key directly. Secret
// options are currently ignored.
func (s *VaultStore) DeleteSecret(ctx context.Context, key string, _ ...confii.SecretOption) error {
	var secretPath string
	if s.kvVersion == 2 {
		secretPath = fmt.Sprintf("%s/metadata/%s", s.mountPoint, key)
	} else {
		secretPath = fmt.Sprintf("%s/%s", s.mountPoint, key)
	}
	_, err := s.client.Logical().DeleteWithContext(ctx, secretPath)
	return err
}

// ListSecrets returns the backend-provided child keys beneath prefix. Vault may
// suffix directory-like entries with "/". The method returns nil, nil when the
// backend provides no list data; result ordering is backend-defined.
func (s *VaultStore) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	listPath := fmt.Sprintf("%s/metadata/%s", s.mountPoint, prefix)
	if s.kvVersion == 1 {
		listPath = fmt.Sprintf("%s/%s", s.mountPoint, prefix)
	}

	secret, err := s.client.Logical().ListWithContext(ctx, listPath)
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}

	keysRaw, ok := secret.Data["keys"]
	if !ok {
		return nil, nil
	}

	data, _ := json.Marshal(keysRaw)
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("%w: decode Vault list response: %w", confii.ErrSecretAccess, err)
	}
	return keys, nil
}
