//go:build vault

package cloud

import (
	"errors"
	"fmt"
	"os"

	confii "github.com/confiify/confii-go"
	"github.com/hashicorp/vault/api"
)

// ErrVaultAuthUnsupported is returned by Vault auth methods that require a
// credential or assertion the consumer must construct out-of-band (signed
// STS request for AWS IAM, Azure AD JWT from IMDS, signed JWT for GCP, OIDC
// callback flow). These flows deliberately accept pre-built credentials so
// the Vault auth API is not coupled to provider-specific signing clients.
//
// Wrap this with [confii.ErrVaultAuth] when surfacing to higher layers; the
// returned errors below already do so.
var ErrVaultAuthUnsupported = errors.New("vault auth method requires a consumer-prepared credential")

// defaultK8sServiceAccountTokenPath is where Kubernetes mounts the projected
// service-account JWT inside a pod. The Vault SDK does NOT auto-discover
// this for you; KubernetesAuth reads it directly when JWT is empty.
const defaultK8sServiceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// TokenAuth authenticates with a static token.
//
// This is the simplest auth method: the Token field is returned verbatim
// without contacting Vault. Use this when the consumer has obtained a
// Vault token through some other channel (CLI login, sidecar injector,
// orchestration platform, etc.) and just wants to thread it into the
// store.
type TokenAuth struct {
	// Token is the static Vault token. Required.
	Token string
}

// Authenticate returns the static token directly without contacting Vault.
func (a *TokenAuth) Authenticate(client *api.Client) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("%w: TokenAuth.Token is empty", confii.ErrVaultAuth)
	}
	return a.Token, nil
}

// AppRoleAuth authenticates via AppRole.
//
// Required fields: RoleID and SecretID. MountPoint defaults to "approle".
// The login is performed against `auth/<MountPoint>/login`.
type AppRoleAuth struct {
	// RoleID identifies the AppRole. Required.
	RoleID string
	// SecretID is the secret credential bound to the role. Required.
	SecretID string
	// MountPoint overrides the auth method mount path (default "approle").
	MountPoint string
}

// Authenticate logs in to Vault using the AppRole auth method with the configured role ID and secret ID.
func (a *AppRoleAuth) Authenticate(client *api.Client) (string, error) {
	if a.RoleID == "" || a.SecretID == "" {
		return "", fmt.Errorf("%w: AppRoleAuth requires RoleID and SecretID", confii.ErrVaultAuth)
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "approle"
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), map[string]any{
		"role_id":   a.RoleID,
		"secret_id": a.SecretID,
	})
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("%w: AppRole login returned no auth payload", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

// LDAPAuth authenticates via LDAP.
//
// Required fields: Username and either Password or PasswordProvider. The
// login is performed against `auth/<MountPoint>/login/<Username>` (default
// MountPoint "ldap"). PasswordProvider is consulted only when Password is
// empty, allowing the caller to pull from a keyring/prompt without keeping
// the secret in memory.
type LDAPAuth struct {
	// Username is the LDAP username. Required.
	Username string
	// Password is the LDAP password. Required if PasswordProvider is nil.
	Password string
	// PasswordProvider is invoked lazily when Password is empty.
	PasswordProvider func() (string, error)
	// MountPoint overrides the auth method mount path (default "ldap").
	MountPoint string
}

// Authenticate logs in to Vault using LDAP credentials, optionally obtaining the password from a provider function.
func (a *LDAPAuth) Authenticate(client *api.Client) (string, error) {
	if a.Username == "" {
		return "", fmt.Errorf("%w: LDAPAuth.Username is empty", confii.ErrVaultAuth)
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "ldap"
	}
	password := a.Password
	if password == "" && a.PasswordProvider != nil {
		var err error
		password, err = a.PasswordProvider()
		if err != nil {
			return "", fmt.Errorf("%w: ldap password provider: %v", confii.ErrVaultAuth, err)
		}
	}
	if password == "" {
		return "", fmt.Errorf("%w: LDAPAuth requires Password or PasswordProvider", confii.ErrVaultAuth)
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login/%s", mp, a.Username), map[string]any{
		"password": password,
	})
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("%w: LDAP login returned no auth payload", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

// JWTAuth authenticates via the generic JWT/OIDC auth method (HCL: jwt).
//
// Required fields: Role and JWT. The JWT must be a serialized OIDC ID token
// or comparable bearer JWT — this auth method does NOT mint the JWT for you.
// MountPoint defaults to "jwt"; for the OIDC variant the consumer typically
// sets it to "oidc" along with the role configured in Vault.
type JWTAuth struct {
	// Role is the Vault JWT role to log in against. Required.
	Role string
	// JWT is the serialized JWT/OIDC ID token. Required.
	JWT string
	// MountPoint overrides the auth method mount path (default "jwt").
	MountPoint string
}

// Authenticate logs in to Vault using a JWT token and the configured role.
func (a *JWTAuth) Authenticate(client *api.Client) (string, error) {
	if a.Role == "" || a.JWT == "" {
		return "", fmt.Errorf("%w: JWTAuth requires Role and JWT", confii.ErrVaultAuth)
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "jwt"
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), map[string]any{
		"role": a.Role,
		"jwt":  a.JWT,
	})
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("%w: JWT login returned no auth payload", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

// KubernetesAuth authenticates via the Kubernetes service-account JWT.
//
// Required fields: Role. The JWT field is optional — if empty, Authenticate
// reads the projected service-account token from TokenPath (or, if TokenPath
// is empty, from the canonical in-pod path
// `/var/run/secrets/kubernetes.io/serviceaccount/token`). MountPoint
// defaults to "kubernetes".
//
// Discovery semantics: when the JWT is read from disk, the file is opened
// at every call (the kubelet rotates the token periodically — caching is
// intentionally avoided so the rotated token reaches Vault on the next
// re-auth).
type KubernetesAuth struct {
	// Role is the Vault Kubernetes role to log in against. Required.
	Role string
	// JWT is the service-account JWT. If empty, falls back to TokenPath.
	JWT string
	// TokenPath overrides the projected SA token path. If empty, the
	// canonical in-pod path is used.
	TokenPath string
	// MountPoint overrides the auth method mount path (default "kubernetes").
	MountPoint string
}

// Authenticate logs in to Vault using a Kubernetes service account JWT
// token and the configured role. When JWT is empty the projected token
// file is read from disk on each invocation.
func (a *KubernetesAuth) Authenticate(client *api.Client) (string, error) {
	if a.Role == "" {
		return "", fmt.Errorf("%w: KubernetesAuth.Role is empty", confii.ErrVaultAuth)
	}
	jwt := a.JWT
	if jwt == "" {
		path := a.TokenPath
		if path == "" {
			path = defaultK8sServiceAccountTokenPath
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("%w: read service account token %q: %v", confii.ErrVaultAuth, path, err)
		}
		jwt = string(data)
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "kubernetes"
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), map[string]any{
		"role": a.Role,
		"jwt":  jwt,
	})
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("%w: Kubernetes login returned no auth payload", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

// AWSIAMAuth authenticates via AWS IAM signed STS GetCallerIdentity.
//
// The consumer is responsible for constructing and signing the STS request
// using the AWS SDK; this struct simply forwards the resulting components
// to Vault. This keeps signing policy and credential discovery in the
// application that owns them.
//
// Required fields:
//
//   - Role
//   - IAMHTTPRequestMethod (typically "POST")
//   - IAMHTTPRequestURL    (base64-encoded https://sts.amazonaws.com/)
//   - IAMHTTPRequestBody   (base64-encoded "Action=GetCallerIdentity&Version=2011-06-15")
//   - IAMHTTPRequestHeaders (base64-encoded JSON object of signed headers,
//     including X-Amz-Date, X-Amz-Security-Token if applicable, and
//     Authorization)
//
// MountPoint defaults to "aws". If the consumer has not yet wired the AWS
// SDK to sign the request, Authenticate returns [ErrVaultAuthUnsupported]
// wrapped with [confii.ErrVaultAuth].
//
// See https://developer.hashicorp.com/vault/docs/auth/aws#iam-authentication-method
// for how to construct the four base64-encoded parameters.
type AWSIAMAuth struct {
	// Role is the Vault AWS role to log in against. Required.
	Role string
	// IAMServerIDHeader is the optional X-Vault-AWS-IAM-Server-ID header
	// value that must match the role's bound_iam_server_id_header_value.
	// When set, the consumer MUST also include the same value in the
	// signed request headers.
	IAMServerIDHeader string
	// IAMHTTPRequestMethod is the HTTP method used to sign the STS request.
	// Required.
	IAMHTTPRequestMethod string
	// IAMHTTPRequestURL is the base64-encoded STS endpoint URL. Required.
	IAMHTTPRequestURL string
	// IAMHTTPRequestBody is the base64-encoded STS request body. Required.
	IAMHTTPRequestBody string
	// IAMHTTPRequestHeaders is the base64-encoded JSON of the signed STS
	// request headers (Authorization, X-Amz-Date, etc). Required.
	IAMHTTPRequestHeaders string
	// MountPoint overrides the auth method mount path (default "aws").
	MountPoint string
}

// Authenticate logs in to Vault using AWS IAM authentication. The consumer
// is expected to have already signed the STS GetCallerIdentity request and
// populated the IAMHTTPRequest* fields.
func (a *AWSIAMAuth) Authenticate(client *api.Client) (string, error) {
	if a.Role == "" {
		return "", fmt.Errorf("%w: AWSIAMAuth.Role is empty", confii.ErrVaultAuth)
	}
	if a.IAMHTTPRequestMethod == "" || a.IAMHTTPRequestURL == "" ||
		a.IAMHTTPRequestBody == "" || a.IAMHTTPRequestHeaders == "" {
		return "", fmt.Errorf("AWSIAMAuth requires a signed STS GetCallerIdentity request "+
			"(populate IAMHTTPRequestMethod/URL/Body/Headers using the AWS SDK): %w",
			errors.Join(confii.ErrVaultAuth, ErrVaultAuthUnsupported))
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "aws"
	}
	data := map[string]any{
		"role":                    a.Role,
		"iam_http_request_method": a.IAMHTTPRequestMethod,
		"iam_request_url":         a.IAMHTTPRequestURL,
		"iam_request_body":        a.IAMHTTPRequestBody,
		"iam_request_headers":     a.IAMHTTPRequestHeaders,
	}
	if a.IAMServerIDHeader != "" {
		data["iam_server_id_header_value"] = a.IAMServerIDHeader
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), data)
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("%w: AWS IAM login returned no auth payload", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

// AzureAuth authenticates via the Azure auth method using an Azure AD JWT.
//
// The consumer is responsible for obtaining the JWT — typically from the
// Instance Metadata Service (IMDS) at
// http://169.254.169.254/metadata/identity/oauth2/token, or by exchanging
// a workload identity federated token via the Azure SDK.
//
// Required fields: Role and JWT. The optional VMName/VMSSName/SubscriptionID/
// ResourceGroupName fields, when set, are forwarded to Vault for additional
// resource-binding checks. MountPoint defaults to "azure".
type AzureAuth struct {
	// Role is the Vault Azure role to log in against. Required.
	Role string
	// JWT is the Azure AD access token (typically resource = vault). Required.
	JWT string
	// Resource overrides the AAD resource the JWT was minted for; informational
	// only — Vault validates the audience claim itself.
	Resource string
	// VMName binds the login to a specific VM (optional).
	VMName string
	// VMSSName binds the login to a specific VM Scale Set (optional).
	VMSSName string
	// SubscriptionID binds the login to a subscription (optional).
	SubscriptionID string
	// ResourceGroupName binds the login to a resource group (optional).
	ResourceGroupName string
	// MountPoint overrides the auth method mount path (default "azure").
	MountPoint string
}

// Authenticate logs in to Vault using an Azure AD JWT and the configured role.
func (a *AzureAuth) Authenticate(client *api.Client) (string, error) {
	if a.Role == "" {
		return "", fmt.Errorf("%w: AzureAuth.Role is empty", confii.ErrVaultAuth)
	}
	if a.JWT == "" {
		return "", fmt.Errorf("AzureAuth requires JWT (acquire via IMDS or azidentity): %w",
			errors.Join(confii.ErrVaultAuth, ErrVaultAuthUnsupported))
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "azure"
	}
	data := map[string]any{
		"role": a.Role,
		"jwt":  a.JWT,
	}
	if a.Resource != "" {
		data["resource"] = a.Resource
	}
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
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), data)
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("%w: Azure login returned no auth payload", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

// GCPAuth authenticates via GCP using a signed JWT (IAM or GCE).
//
// The consumer must produce a signed JWT using the GCP IAM API
// (projects.serviceAccounts.signJwt) or the GCE metadata server's
// service-accounts/<email>/identity endpoint.
//
// Required fields: Role and JWT. MountPoint defaults to "gcp".
type GCPAuth struct {
	// Role is the Vault GCP role to log in against. Required.
	Role string
	// JWT is the signed JWT minted by IAM signJwt or the GCE identity
	// endpoint. Required.
	JWT string
	// MountPoint overrides the auth method mount path (default "gcp").
	MountPoint string
}

// Authenticate logs in to Vault using GCP IAM or GCE-issued JWT.
func (a *GCPAuth) Authenticate(client *api.Client) (string, error) {
	if a.Role == "" {
		return "", fmt.Errorf("%w: GCPAuth.Role is empty", confii.ErrVaultAuth)
	}
	if a.JWT == "" {
		return "", fmt.Errorf("GCPAuth requires JWT (mint via IAM signJwt or GCE identity endpoint): %w",
			errors.Join(confii.ErrVaultAuth, ErrVaultAuthUnsupported))
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "gcp"
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), map[string]any{
		"role": a.Role,
		"jwt":  a.JWT,
	})
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("%w: GCP login returned no auth payload", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

// OIDCAuth authenticates via the interactive OIDC callback flow.
//
// DEFERRED — interactive flow requires browser callback handling.
//
// The OIDC callback flow requires:
//
//  1. Calling auth/<mount>/oidc/auth_url to obtain an authorization URL.
//  2. Driving the user's browser through the IdP and capturing the
//     redirect with `state` and `code` query parameters.
//  3. Posting them to auth/<mount>/oidc/callback to mint the Vault token.
//
// This is a UX flow, not a service-to-service flow, and confii-go is
// designed for service-to-service auth. Authenticate therefore returns
// [ErrVaultAuthUnsupported] wrapped with [confii.ErrVaultAuth] and
// suggests the supported alternatives:
//
//   - For machine workloads, use [JWTAuth] with a pre-issued ID token
//     (the OIDC mount accepts JWT logins as well: set MountPoint = "oidc").
//   - For human/CLI flows, run `vault login -method=oidc` externally and
//     pass the resulting token via [TokenAuth].
type OIDCAuth struct {
	// Role is the Vault OIDC role. Used only by the diagnostic error.
	Role string
	// MountPoint overrides the auth method mount path (default "oidc").
	MountPoint string
}

// Authenticate is not yet supported; see the [OIDCAuth] doc comment for the
// recommended alternatives. The error wraps both [confii.ErrVaultAuth] and
// [ErrVaultAuthUnsupported] so callers can distinguish "configuration is
// wrong" from "this flow is intentionally not implemented".
func (a *OIDCAuth) Authenticate(client *api.Client) (string, error) {
	_ = client
	return "", fmt.Errorf("OIDCAuth interactive callback flow is not implemented; "+
		"use JWTAuth with a pre-issued ID token, or TokenAuth with a token minted via `vault login -method=oidc`: %w",
		errors.Join(confii.ErrVaultAuth, ErrVaultAuthUnsupported))
}
