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
)

// AWSIAMSignedRequestAuth submits an IAM GetCallerIdentity request that was
// signed outside Confii. Prefer [AWSIAMAuth], which delegates credential
// discovery and signing to Vault's official AWS auth module. This explicit
// form is intended for applications that already own signing through a custom
// AWS SDK v2 credential provider or an external signing boundary.
type AWSIAMSignedRequestAuth struct {
	// Role is the optional Vault AWS role to log in against.
	Role string
	// IAMServerIDHeader is the optional server ID header value bound by the
	// Vault AWS role.
	IAMServerIDHeader string
	// IAMHTTPRequestMethod is the HTTP method from the signed STS request,
	// normally POST.
	IAMHTTPRequestMethod string
	// IAMHTTPRequestURL is the base64-encoded signed STS request URL.
	IAMHTTPRequestURL string
	// IAMHTTPRequestBody is the base64-encoded signed STS request body.
	IAMHTTPRequestBody string
	// IAMHTTPRequestHeaders is the base64-encoded JSON representation of the
	// signed STS request headers.
	IAMHTTPRequestHeaders string
	// MountPoint overrides the auth method mount path (default "aws").
	MountPoint string
}

// Authenticate submits the complete signed IAM request to Vault. All four
// IAMHTTPRequest fields are required and are expected to use Vault's
// base64-encoded AWS login representation.
func (a *AWSIAMSignedRequestAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	if a == nil {
		return "", fmt.Errorf("%w: AWSIAMSignedRequestAuth is nil", confii.ErrVaultAuth)
	}
	if a.IAMHTTPRequestMethod == "" || a.IAMHTTPRequestURL == "" ||
		a.IAMHTTPRequestBody == "" || a.IAMHTTPRequestHeaders == "" {
		return "", fmt.Errorf("%w: AWSIAMSignedRequestAuth requires method, URL, body, and headers", confii.ErrVaultAuth)
	}
	data := map[string]any{
		"iam_http_request_method": a.IAMHTTPRequestMethod,
		"iam_request_url":         a.IAMHTTPRequestURL,
		"iam_request_body":        a.IAMHTTPRequestBody,
		"iam_request_headers":     a.IAMHTTPRequestHeaders,
	}
	if a.Role != "" {
		data["role"] = a.Role
	}
	if a.IAMServerIDHeader != "" {
		data["iam_server_id_header_value"] = a.IAMServerIDHeader
	}
	return authenticateVaultLoginPayload(ctx, client, a.MountPoint, "aws", data)
}

// AzureJWTAuth submits a caller-provided Azure identity JWT and the resource
// metadata Vault uses to validate it. Prefer [AzureAuth] on Azure compute,
// where the official Vault module obtains both the JWT and metadata from IMDS.
// AzureJWTAuth is intended for workload identity or externally brokered token
// flows that the official module cannot acquire itself.
type AzureJWTAuth struct {
	// Role is the Vault Azure role to log in against. Required.
	Role string
	// JWT is the externally acquired Azure identity token. Required.
	JWT string
	// VMName is the Azure virtual machine name used by the Vault role binding.
	VMName string
	// VMSSName is the Azure virtual machine scale set name used by the Vault
	// role binding.
	VMSSName string
	// SubscriptionID is the Azure subscription containing the compute identity.
	SubscriptionID string
	// ResourceGroupName is the Azure resource group containing the compute
	// identity.
	ResourceGroupName string
	// MountPoint overrides the auth method mount path (default "azure").
	MountPoint string
}

// Authenticate submits the supplied Azure identity and binding metadata to
// Vault. Role and JWT are required. The VM or VMSS binding fields required by
// the configured Vault role must also be supplied by the caller.
func (a *AzureJWTAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	if a == nil || strings.TrimSpace(a.Role) == "" || strings.TrimSpace(a.JWT) == "" {
		return "", fmt.Errorf("%w: AzureJWTAuth requires Role and JWT", confii.ErrVaultAuth)
	}
	data := map[string]any{"role": a.Role, "jwt": a.JWT}
	if a.VMName != "" {
		data["vm_name"] = a.VMName
	}
	if a.VMSSName != "" {
		data["vmss_name"] = a.VMSSName
	}
	if a.SubscriptionID != "" {
		data["subscription_id"] = a.SubscriptionID
	}
	if a.ResourceGroupName != "" {
		data["resource_group_name"] = a.ResourceGroupName
	}
	return authenticateVaultLoginPayload(ctx, client, a.MountPoint, "azure", data)
}

func authenticateVaultLoginPayload(
	ctx context.Context,
	client *api.Client,
	mountPoint string,
	defaultMountPoint string,
	data map[string]any,
) (string, error) {
	if err := vaultAuthContextError(ctx); err != nil {
		return "", err
	}
	if client == nil {
		return "", fmt.Errorf("%w: Vault client is nil", confii.ErrVaultAuth)
	}
	mountPoint = strings.Trim(mountPoint, "/")
	if mountPoint == "" {
		mountPoint = defaultMountPoint
	}
	secret, err := client.Logical().WriteWithContext(ctx, fmt.Sprintf("auth/%s/login", mountPoint), data)
	if err != nil {
		return "", fmt.Errorf("%w: Vault login at auth/%s/login: %w", confii.ErrVaultAuth, mountPoint, err)
	}
	if secret == nil || secret.Auth == nil || strings.TrimSpace(secret.Auth.ClientToken) == "" {
		return "", fmt.Errorf("%w: Vault login returned no client token", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}
