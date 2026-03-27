//go:build vault

package cloud

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	confii "github.com/confiify/confii-go"
	"github.com/hashicorp/vault/api"
)

// HashiCorpVault implements SecretStore for HashiCorp Vault.
type HashiCorpVault struct {
	client     *api.Client
	mountPoint string
	kvVersion  int
	namespace  string
}

// VaultOption configures a HashiCorpVault store.
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
}

// VaultAuthMethod authenticates with Vault.
type VaultAuthMethod interface {
	Authenticate(client *api.Client) (string, error)
}

func WithVaultURL(url string) VaultOption          { return func(c *vaultConfig) { c.URL = url } }
func WithVaultToken(token string) VaultOption      { return func(c *vaultConfig) { c.Token = token } }
func WithVaultNamespace(ns string) VaultOption     { return func(c *vaultConfig) { c.Namespace = ns } }
func WithVaultMountPoint(mp string) VaultOption    { return func(c *vaultConfig) { c.MountPoint = mp } }
func WithVaultKVVersion(v int) VaultOption         { return func(c *vaultConfig) { c.KVVersion = v } }
func WithVaultVerify(v bool) VaultOption           { return func(c *vaultConfig) { c.Verify = v } }
func WithVaultAuth(am VaultAuthMethod) VaultOption { return func(c *vaultConfig) { c.AuthMethod = am } }
func WithVaultAppRole(roleID, secretID string) VaultOption {
	return func(c *vaultConfig) {
		c.RoleID = roleID
		c.SecretID = secretID
	}
}

// NewHashiCorpVault creates a new HashiCorp Vault secret store.
func NewHashiCorpVault(opts ...VaultOption) (*HashiCorpVault, error) {
	cfg := &vaultConfig{
		URL:        "http://127.0.0.1:8200",
		MountPoint: "secret",
		KVVersion:  2,
		Verify:     true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	vaultCfg := api.DefaultConfig()
	vaultCfg.Address = cfg.URL
	if !cfg.Verify {
		vaultCfg.HttpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	client, err := api.NewClient(vaultCfg)
	if err != nil {
		return nil, fmt.Errorf("vault client: %w", err)
	}

	if cfg.Namespace != "" {
		client.SetNamespace(cfg.Namespace)
	}

	// Authenticate: auth_method > token > role_id+secret_id
	switch {
	case cfg.AuthMethod != nil:
		token, err := cfg.AuthMethod.Authenticate(client)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", confii.ErrVaultAuth, err)
		}
		client.SetToken(token)
	case cfg.Token != "":
		client.SetToken(cfg.Token)
	case cfg.RoleID != "" && cfg.SecretID != "":
		secret, err := client.Logical().Write("auth/approle/login", map[string]any{
			"role_id":   cfg.RoleID,
			"secret_id": cfg.SecretID,
		})
		if err != nil {
			return nil, fmt.Errorf("%w: approle login: %v", confii.ErrVaultAuth, err)
		}
		client.SetToken(secret.Auth.ClientToken)
	}

	return &HashiCorpVault{
		client:     client,
		mountPoint: cfg.MountPoint,
		kvVersion:  cfg.KVVersion,
		namespace:  cfg.Namespace,
	}, nil
}

func (s *HashiCorpVault) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	o := confii.ResolveSecretOptions(opts...)

	// Support "path:field" syntax.
	path, field, _ := strings.Cut(key, ":")

	var secretPath string
	if s.kvVersion == 2 {
		secretPath = fmt.Sprintf("%s/data/%s", s.mountPoint, path)
	} else {
		secretPath = fmt.Sprintf("%s/%s", s.mountPoint, path)
	}

	// Read with version for KV v2.
	var secret *api.Secret
	var err error
	if s.kvVersion == 2 && o.Version != "" {
		secret, err = s.client.Logical().ReadWithDataWithContext(ctx, secretPath, map[string][]string{
			"version": {o.Version},
		})
	} else {
		secret, err = s.client.Logical().ReadWithContext(ctx, secretPath)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", confii.ErrSecretAccess, err)
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
	if field != "" {
		v, ok := data[field]
		if !ok {
			return nil, fmt.Errorf("%w: field %q not found in secret %s", confii.ErrSecretValidation, field, path)
		}
		return v, nil
	}

	return data, nil
}

func (s *HashiCorpVault) SetSecret(ctx context.Context, key string, value any, _ ...confii.SecretOption) error {
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

func (s *HashiCorpVault) DeleteSecret(ctx context.Context, key string, _ ...confii.SecretOption) error {
	var secretPath string
	if s.kvVersion == 2 {
		secretPath = fmt.Sprintf("%s/metadata/%s", s.mountPoint, key)
	} else {
		secretPath = fmt.Sprintf("%s/%s", s.mountPoint, key)
	}
	_, err := s.client.Logical().DeleteWithContext(ctx, secretPath)
	return err
}

func (s *HashiCorpVault) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
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
	json.Unmarshal(data, &keys)
	return keys, nil
}
