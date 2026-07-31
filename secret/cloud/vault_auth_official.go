// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"fmt"
	"strings"

	confii "github.com/confiify/confii-go/v2"
	"github.com/hashicorp/vault/api"
	approleauth "github.com/hashicorp/vault/api/auth/approle"
	awsauth "github.com/hashicorp/vault/api/auth/aws"
	azureauth "github.com/hashicorp/vault/api/auth/azure"
	gcpauth "github.com/hashicorp/vault/api/auth/gcp"
	kubernetesauth "github.com/hashicorp/vault/api/auth/kubernetes"
)

// authenticateOfficialVaultMethod executes an auth method implemented by the
// official Vault API modules and converts its response to Confii's token-based
// VaultAuthMethod contract. Confii retains validation and stable error
// classification while the provider module owns credential discovery,
// request signing, and login payload construction.
func authenticateOfficialVaultMethod(ctx context.Context, client *api.Client, method api.AuthMethod) (string, error) {
	if err := vaultAuthContextError(ctx); err != nil {
		return "", err
	}
	if client == nil {
		return "", fmt.Errorf("%w: Vault client is nil", confii.ErrVaultAuth)
	}
	if method == nil {
		return "", fmt.Errorf("%w: Vault auth method is nil", confii.ErrVaultAuth)
	}

	secret, err := client.Auth().Login(ctx, method)
	if err != nil {
		return "", fmt.Errorf("%w: official Vault auth login: %w", confii.ErrVaultAuth, err)
	}
	if secret == nil || secret.Auth == nil || strings.TrimSpace(secret.Auth.ClientToken) == "" {
		return "", fmt.Errorf("%w: Vault login returned no client token", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

func newOfficialAppRoleMethod(auth *AppRoleAuth) (api.AuthMethod, error) {
	if auth == nil || strings.TrimSpace(auth.RoleID) == "" {
		return nil, fmt.Errorf("%w: AppRoleAuth.RoleID is empty", confii.ErrVaultAuth)
	}
	if countNonEmpty(auth.SecretID, auth.SecretIDFile, auth.SecretIDEnv) != 1 {
		return nil, fmt.Errorf("%w: AppRoleAuth requires exactly one SecretID source", confii.ErrVaultAuth)
	}

	secretID := &approleauth.SecretID{
		FromString: auth.SecretID,
		FromFile:   auth.SecretIDFile,
		FromEnv:    auth.SecretIDEnv,
	}
	var opts []approleauth.LoginOption
	if auth.MountPoint != "" {
		opts = append(opts, approleauth.WithMountPath(auth.MountPoint))
	}
	if auth.WrappingToken {
		opts = append(opts, approleauth.WithWrappingToken())
	}
	method, err := approleauth.NewAppRoleAuth(auth.RoleID, secretID, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: configure AppRole auth: %w", confii.ErrVaultAuth, err)
	}
	return method, nil
}

func newOfficialKubernetesMethod(auth *KubernetesAuth) (api.AuthMethod, error) {
	if auth == nil || strings.TrimSpace(auth.Role) == "" {
		return nil, fmt.Errorf("%w: KubernetesAuth.Role is empty", confii.ErrVaultAuth)
	}
	if countNonEmpty(auth.JWT, auth.TokenEnv, auth.TokenPath) > 1 {
		return nil, fmt.Errorf("%w: KubernetesAuth accepts at most one token source", confii.ErrVaultAuth)
	}
	var opts []kubernetesauth.LoginOption
	if auth.MountPoint != "" {
		opts = append(opts, kubernetesauth.WithMountPath(auth.MountPoint))
	}
	switch {
	case auth.JWT != "":
		opts = append(opts, kubernetesauth.WithServiceAccountToken(strings.TrimSpace(auth.JWT)))
	case auth.TokenEnv != "":
		opts = append(opts, kubernetesauth.WithServiceAccountTokenEnv(auth.TokenEnv))
	case auth.TokenPath != "":
		opts = append(opts, kubernetesauth.WithServiceAccountTokenPath(auth.TokenPath))
	}
	method, err := kubernetesauth.NewKubernetesAuth(auth.Role, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: configure Kubernetes auth: %w", confii.ErrVaultAuth, err)
	}
	return method, nil
}

func countNonEmpty(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func newOfficialAWSMethod(auth *AWSIAMAuth) (api.AuthMethod, error) {
	if auth == nil {
		return nil, fmt.Errorf("%w: AWSIAMAuth is nil", confii.ErrVaultAuth)
	}
	opts := []awsauth.LoginOption{awsauth.WithIAMAuth()}
	if auth.Role != "" {
		opts = append(opts, awsauth.WithRole(auth.Role))
	}
	if auth.MountPoint != "" {
		opts = append(opts, awsauth.WithMountPath(auth.MountPoint))
	}
	if auth.IAMServerIDHeader != "" {
		opts = append(opts, awsauth.WithIAMServerIDHeader(auth.IAMServerIDHeader))
	}
	if auth.Region != "" {
		opts = append(opts, awsauth.WithRegion(auth.Region))
	}
	method, err := awsauth.NewAWSAuth(opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: configure AWS IAM auth: %w", confii.ErrVaultAuth, err)
	}
	return method, nil
}

func newOfficialAzureMethod(auth *AzureAuth) (api.AuthMethod, error) {
	if auth == nil || strings.TrimSpace(auth.Role) == "" {
		return nil, fmt.Errorf("%w: AzureAuth.Role is empty", confii.ErrVaultAuth)
	}
	var opts []azureauth.LoginOption
	if auth.MountPoint != "" {
		opts = append(opts, azureauth.WithMountPath(auth.MountPoint))
	}
	if auth.Resource != "" {
		opts = append(opts, azureauth.WithResource(auth.Resource))
	}
	method, err := azureauth.NewAzureAuth(auth.Role, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: configure Azure auth: %w", confii.ErrVaultAuth, err)
	}
	return method, nil
}

func newOfficialGCPMethod(auth *GCPAuth) (api.AuthMethod, error) {
	if auth == nil || strings.TrimSpace(auth.Role) == "" {
		return nil, fmt.Errorf("%w: GCPAuth.Role is empty", confii.ErrVaultAuth)
	}
	var opts []gcpauth.LoginOption
	if auth.MountPoint != "" {
		opts = append(opts, gcpauth.WithMountPath(auth.MountPoint))
	}
	switch strings.ToLower(strings.TrimSpace(auth.AuthType)) {
	case "", "gce":
		opts = append(opts, gcpauth.WithGCEAuth())
	case "iam":
		if strings.TrimSpace(auth.ServiceAccountEmail) == "" {
			return nil, fmt.Errorf("%w: GCP IAM auth requires ServiceAccountEmail", confii.ErrVaultAuth)
		}
		opts = append(opts, gcpauth.WithIAMAuth(auth.ServiceAccountEmail))
	default:
		return nil, fmt.Errorf("%w: unsupported GCP auth type %q", confii.ErrVaultAuth, auth.AuthType)
	}
	method, err := gcpauth.NewGCPAuth(auth.Role, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: configure GCP auth: %w", confii.ErrVaultAuth, err)
	}
	return method, nil
}
