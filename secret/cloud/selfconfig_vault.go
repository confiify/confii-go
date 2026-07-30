// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	confii "github.com/confiify/confii-go/v2"
)

func init() {
	confii.RegisterSelfConfigSecretProvider("vault", newSelfConfigVault)
}

func newSelfConfigVault(ctx context.Context, cfg map[string]any) (confii.SecretReader, error) {
	address := selfString(cfg, "address", "url")
	if address == "" {
		address = os.Getenv("VAULT_ADDR")
	}
	if address == "" {
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
	if namespace := selfString(cfg, "namespace"); namespace != "" {
		opts = append(opts, WithVaultNamespace(namespace))
	}
	if mount := selfString(cfg, "mount_point", "mount"); mount != "" {
		opts = append(opts, WithVaultMountPoint(mount))
	}

	token := selfString(cfg, "token")
	if token == "" {
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
	store, err := NewHashiCorpVaultWithContext(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return selfConfigStoreAdapter{get: func(ctx context.Context, key string, secretOpts ...confii.SecretOption) (any, error) {
		return store.GetSecret(ctx, key, secretOpts...)
	}}, nil
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
		return &AppRoleAuth{RoleID: selfString(cfg, "role_id"), SecretID: selfString(cfg, "secret_id"), MountPoint: mount}, nil
	case "ldap":
		return &LDAPAuth{Username: selfString(cfg, "username"), Password: selfString(cfg, "password"), MountPoint: mount}, nil
	case "jwt":
		return &JWTAuth{Role: selfString(cfg, "role"), JWT: selfString(cfg, "jwt"), MountPoint: mount}, nil
	case "kubernetes", "k8s":
		return &KubernetesAuth{Role: selfString(cfg, "role"), JWT: selfString(cfg, "jwt"), TokenPath: selfString(cfg, "token_path"), MountPoint: mount}, nil
	case "aws", "aws_iam", "awsiam":
		return &AWSIAMAuth{
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
			Role:              selfString(cfg, "role"),
			JWT:               selfString(cfg, "jwt"),
			Resource:          selfString(cfg, "resource"),
			VMName:            selfString(cfg, "vm_name"),
			VMSSName:          selfString(cfg, "vmss_name"),
			SubscriptionID:    selfString(cfg, "subscription_id"),
			ResourceGroupName: selfString(cfg, "resource_group_name"),
			MountPoint:        mount,
		}, nil
	case "gcp":
		return &GCPAuth{Role: selfString(cfg, "role"), JWT: selfString(cfg, "jwt"), MountPoint: mount}, nil
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
