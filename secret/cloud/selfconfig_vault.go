// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	confii "github.com/confiify/confii-go/v2"
)

func init() {
	confii.RegisterSelfConfigSecretProvider("vault", newSelfConfigVault)
}

// vaultSelfConfigKeys is every setting the vault provider understands. Strict
// mode rejects anything outside it, so a typo fails loudly instead of leaving
// the intended setting silently at its default.
var vaultSelfConfigKeys = map[string]struct{}{
	"strict": {}, "address": {}, "url": {}, "namespace": {},
	"mount_point": {}, "mount": {}, "kv_version": {}, "verify": {},
	"token": {}, "auth": {}, "timeout": {}, "retry_limit": {},
	"proxy": {}, "tls": {}, "follow_redirects": {},
}

// vaultSelfConfigOptions maps declarative settings onto constructor
// options. It is separate from construction so the mapping can be verified
// without standing up a client.
func vaultSelfConfigOptions(cfg map[string]any) ([]VaultOption, error) {
	strict, err := selfBool(cfg, "strict", false)
	if err != nil {
		return nil, err
	}
	if strict {
		if err := rejectUnknownVaultKeys(cfg); err != nil {
			return nil, err
		}
	}

	address := selfString(cfg, "address", "url")
	if address == "" && !strict {
		address = os.Getenv("VAULT_ADDR")
	}
	if address == "" {
		if strict {
			return nil, fmt.Errorf(
				"%w: address must be declared; VAULT_ADDR is not consulted in strict mode",
				ErrVaultStrictConfiguration)
		}
		return nil, fmt.Errorf("vault provider requires address (or VAULT_ADDR)")
	}
	verify, err := selfBool(cfg, "verify", true)
	if err != nil {
		return nil, err
	}
	kvVersion, err := selfInt(cfg, "kv_version", 2)
	if err != nil || (kvVersion != 1 && kvVersion != 2) {
		return nil, fmt.Errorf("vault provider kv_version must be 1 or 2")
	}
	opts := []VaultOption{WithVaultURL(address), WithVaultVerify(verify), WithVaultKVVersion(kvVersion)}
	if strict {
		// A configuration declaring itself the sole authority must not have
		// its transport shaped by the environment either.
		opts = append(opts, WithVaultHermetic())
	}
	transportOpts, err := vaultSelfTransportOptions(cfg)
	if err != nil {
		return nil, err
	}
	opts = append(opts, transportOpts...)
	if namespace := selfString(cfg, "namespace"); namespace != "" {
		opts = append(opts, WithVaultNamespace(namespace))
	}
	if mount := selfString(cfg, "mount_point", "mount"); mount != "" {
		opts = append(opts, WithVaultMountPoint(mount))
	}

	token := selfString(cfg, "token")
	if token == "" && !strict {
		token = os.Getenv("VAULT_TOKEN")
	}
	authName, authCfg, err := vaultSelfAuthConfig(cfg)
	if err != nil {
		return nil, err
	}
	if authName == "" && token != "" {
		authName = "token"
	}
	if authName != "" {
		auth, err := buildVaultSelfAuth(authName, authCfg, token)
		if err != nil {
			return nil, err
		}
		opts = append(opts, WithVaultAuth(auth))
	}
	return opts, nil
}

func newSelfConfigVault(ctx context.Context, cfg map[string]any) (confii.SecretReader, error) {
	opts, err := vaultSelfConfigOptions(cfg)
	if err != nil {
		return nil, err
	}
	store, err := NewHashiCorpVaultWithContext(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return selfConfigStoreAdapter{
		get: func(ctx context.Context, key string, secretOpts ...confii.SecretOption) (any, error) {
			return store.GetSecret(ctx, key, secretOpts...)
		},
		// Without this the store would outlive the configuration that built it.
		close: store.Close,
	}, nil
}

// ErrVaultStrictConfiguration reports a strict vault provider whose settings
// are incomplete or not understood.
//
// Strict mode makes declared configuration the sole authority: VAULT_ADDR and
// VAULT_TOKEN are not consulted, the transport is built hermetically, and an
// unrecognized setting is an error rather than a silently ignored key. Errors
// name the setting at fault and never the value, which may be a credential.
var ErrVaultStrictConfiguration = errors.New("vault provider: strict configuration")

func rejectUnknownVaultKeys(cfg map[string]any) error {
	unknown := make([]string, 0, len(cfg))
	for key := range cfg {
		if _, ok := vaultSelfConfigKeys[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%w: unrecognized setting %s",
		ErrVaultStrictConfiguration, strings.Join(unknown, ", "))
}

// vaultSelfTransportOptions maps the declarative transport settings. They apply
// in both modes; strict mode only removes the environment fallbacks.
func vaultSelfTransportOptions(cfg map[string]any) ([]VaultOption, error) {
	var opts []VaultOption

	if raw := selfString(cfg, "timeout"); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("vault provider timeout: %w", err)
		}
		opts = append(opts, WithVaultTimeout(timeout))
	}
	if _, ok := cfg["retry_limit"]; ok {
		limit, err := selfInt(cfg, "retry_limit", 0)
		if err != nil {
			return nil, err
		}
		if limit < 0 {
			return nil, fmt.Errorf("vault provider retry_limit must not be negative")
		}
		opts = append(opts, WithVaultRetryLimit(limit))
	}
	if proxy := selfString(cfg, "proxy"); proxy != "" {
		opts = append(opts, WithVaultProxy(proxy))
	}
	if _, ok := cfg["follow_redirects"]; ok {
		follow, err := selfBool(cfg, "follow_redirects", false)
		if err != nil {
			return nil, err
		}
		opts = append(opts, WithVaultFollowRedirects(follow))
	}

	tlsCfg, ok := cfg["tls"].(map[string]any)
	if !ok {
		return opts, nil
	}
	material := VaultTLS{
		CACertPEM:     []byte(selfString(tlsCfg, "ca_cert_pem")),
		ClientCertPEM: []byte(selfString(tlsCfg, "client_cert_pem")),
		ClientKeyPEM:  []byte(selfString(tlsCfg, "client_key_pem")),
		ServerName:    selfString(tlsCfg, "server_name"),
	}
	return append(opts, WithVaultTLS(material)), nil
}

func vaultSelfAuthConfig(cfg map[string]any) (string, map[string]any, error) {
	raw, ok := cfg["auth"]
	if !ok {
		return "", cfg, nil
	}
	switch value := raw.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(value)), cfg, nil
	case map[string]any:
		name := selfString(value, "method", "type")
		if name == "" {
			return "", nil, fmt.Errorf("vault auth map requires method")
		}
		return strings.ToLower(name), value, nil
	default:
		return "", nil, fmt.Errorf("vault auth must be a method name or map")
	}
}

func buildVaultSelfAuth(name string, cfg map[string]any, token string) (VaultAuthMethod, error) {
	mount := selfString(cfg, "mount_point", "mount")
	switch name {
	case "token":
		if token == "" {
			token = selfString(cfg, "token")
		}
		return &TokenAuth{Token: token}, nil
	case "approle":
		wrappingToken, err := selfBool(cfg, "wrapping_token", false)
		if err != nil {
			return nil, err
		}
		return &AppRoleAuth{
			RoleID:        selfString(cfg, "role_id"),
			SecretID:      selfString(cfg, "secret_id"),
			SecretIDFile:  selfString(cfg, "secret_id_file"),
			SecretIDEnv:   selfString(cfg, "secret_id_env"),
			WrappingToken: wrappingToken,
			MountPoint:    mount,
		}, nil
	case "ldap":
		return &LDAPAuth{Username: selfString(cfg, "username"), Password: selfString(cfg, "password"), MountPoint: mount}, nil
	case "jwt":
		return &JWTAuth{Role: selfString(cfg, "role"), JWT: selfString(cfg, "jwt"), MountPoint: mount}, nil
	case "kubernetes", "k8s":
		return &KubernetesAuth{
			Role:       selfString(cfg, "role"),
			JWT:        selfString(cfg, "jwt"),
			TokenPath:  selfString(cfg, "token_path"),
			TokenEnv:   selfString(cfg, "token_env"),
			MountPoint: mount,
		}, nil
	case "aws", "aws_iam", "awsiam":
		return &AWSIAMAuth{
			Role:              selfString(cfg, "role"),
			IAMServerIDHeader: selfString(cfg, "iam_server_id_header"),
			Region:            selfString(cfg, "region"),
			MountPoint:        mount,
		}, nil
	case "aws_signed_request":
		return &AWSIAMSignedRequestAuth{
			Role:                  selfString(cfg, "role"),
			IAMServerIDHeader:     selfString(cfg, "iam_server_id_header"),
			IAMHTTPRequestMethod:  selfString(cfg, "iam_http_request_method"),
			IAMHTTPRequestURL:     selfString(cfg, "iam_request_url"),
			IAMHTTPRequestBody:    selfString(cfg, "iam_request_body"),
			IAMHTTPRequestHeaders: selfString(cfg, "iam_request_headers"),
			MountPoint:            mount,
		}, nil
	case "azure":
		return &AzureAuth{
			Role:       selfString(cfg, "role"),
			Resource:   selfString(cfg, "resource"),
			MountPoint: mount,
		}, nil
	case "azure_jwt":
		return &AzureJWTAuth{
			Role:              selfString(cfg, "role"),
			JWT:               selfString(cfg, "jwt"),
			VMName:            selfString(cfg, "vm_name"),
			VMSSName:          selfString(cfg, "vmss_name"),
			SubscriptionID:    selfString(cfg, "subscription_id"),
			ResourceGroupName: selfString(cfg, "resource_group_name"),
			MountPoint:        mount,
		}, nil
	case "gcp":
		return &GCPAuth{
			Role:                selfString(cfg, "role"),
			AuthType:            selfString(cfg, "auth_type"),
			ServiceAccountEmail: selfString(cfg, "service_account_email"),
			MountPoint:          mount,
		}, nil
	case "gcp_jwt":
		if mount == "" {
			mount = "gcp"
		}
		return &JWTAuth{Role: selfString(cfg, "role"), JWT: selfString(cfg, "jwt"), MountPoint: mount}, nil
	case "oidc":
		timeout := time.Duration(0)
		if seconds, err := selfInt(cfg, "callback_timeout_seconds", 0); err != nil {
			return nil, err
		} else if seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
		return &OIDCAuth{Role: selfString(cfg, "role"), MountPoint: mount, RedirectURI: selfString(cfg, "redirect_uri"), CallbackTimeout: timeout}, nil
	default:
		return nil, fmt.Errorf("unsupported vault auth method %q", name)
	}
}
