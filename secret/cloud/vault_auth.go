//go:build vault

package cloud

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/hashicorp/vault/api"
	"github.com/pkg/browser"
)

// ErrVaultAuthUnsupported is returned by Vault auth methods that require a
// credential or assertion the consumer must construct out-of-band (signed
// STS request for AWS IAM, Azure AD JWT from IMDS, or signed JWT for GCP).
// These flows deliberately accept pre-built credentials so
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
		jwt = strings.TrimSpace(string(data))
		if jwt == "" {
			return "", fmt.Errorf("%w: service account token %q is empty", confii.ErrVaultAuth, path)
		}
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

// OIDCAuth authenticates through Vault's browser-based OIDC authorization
// code flow. By default it listens on the loopback RedirectURI, opens the
// authorization URL in the user's browser, validates the returned state and
// nonce, and exchanges the callback parameters with Vault for a client token.
//
// A custom CallbackProvider can be supplied by headless applications or UIs.
// It receives Vault's authorization URL and must return the full redirect URL
// received from the identity provider. RedirectURI still has to match one of
// the role's allowed_redirect_uris in Vault and in the OIDC provider.
type OIDCAuth struct {
	// Role is the Vault OIDC role. It may be empty when the auth mount has a
	// default_role configured.
	Role string
	// MountPoint overrides the auth method mount path (default "oidc").
	MountPoint string
	// RedirectURI defaults to http://localhost:8250/oidc/callback.
	RedirectURI string
	// ClientNonce optionally supplies the nonce that binds auth_url to the
	// callback exchange. A cryptographically random value is generated when
	// this field is empty.
	ClientNonce string
	// CallbackTimeout bounds the built-in loopback callback wait. It defaults
	// to two minutes.
	CallbackTimeout time.Duration
	// OpenBrowser overrides the browser launcher, primarily for embedded UIs
	// and tests. It defaults to browser.OpenURL.
	OpenBrowser func(string) error
	// CallbackProvider replaces the built-in loopback HTTP listener. The
	// returned string must be the complete callback URL, including state,
	// nonce, and code query parameters.
	CallbackProvider func(authorizationURL string) (callbackURL string, err error)
}

// Authenticate completes Vault's OIDC auth_url and callback protocol.
func (a *OIDCAuth) Authenticate(client *api.Client) (string, error) {
	if client == nil {
		return "", fmt.Errorf("%w: OIDCAuth requires a Vault client", confii.ErrVaultAuth)
	}
	mp := strings.Trim(a.MountPoint, "/")
	if mp == "" {
		mp = "oidc"
	}
	redirectURI := a.RedirectURI
	if redirectURI == "" {
		redirectURI = "http://localhost:8250/oidc/callback"
	}
	redirect, err := url.Parse(redirectURI)
	if err != nil || redirect.Scheme == "" || redirect.Host == "" {
		return "", fmt.Errorf("%w: invalid OIDC redirect URI %q", confii.ErrVaultAuth, redirectURI)
	}

	clientNonce := a.ClientNonce
	if clientNonce == "" {
		clientNonce, err = randomOIDCNonce()
		if err != nil {
			return "", fmt.Errorf("%w: generate OIDC client nonce: %v", confii.ErrVaultAuth, err)
		}
	}

	var callbackURL func(string) (string, error)
	var listener net.Listener
	if a.CallbackProvider != nil {
		callbackURL = a.CallbackProvider
	} else {
		listener, err = listenOIDCCallback(redirect)
		if err != nil {
			return "", err
		}
		defer listener.Close()
	}

	payload := map[string]any{
		"redirect_uri": redirectURI,
		"client_nonce": clientNonce,
	}
	if a.Role != "" {
		payload["role"] = a.Role
	}
	authURLSecret, err := client.Logical().Write(fmt.Sprintf("auth/%s/oidc/auth_url", mp), payload)
	if err != nil {
		return "", fmt.Errorf("%w: request OIDC authorization URL: %v", confii.ErrVaultAuth, err)
	}
	authURL, ok := secretString(authURLSecret, "auth_url")
	if !ok {
		return "", fmt.Errorf("%w: OIDC auth_url response did not contain auth_url", confii.ErrVaultAuth)
	}
	expected, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("%w: Vault returned an invalid OIDC authorization URL", confii.ErrVaultAuth)
	}
	expectedState := expected.Query().Get("state")
	expectedNonce := expected.Query().Get("nonce")
	if expectedState == "" || expectedNonce == "" {
		return "", fmt.Errorf("%w: Vault OIDC authorization URL omitted state or nonce", confii.ErrVaultAuth)
	}

	var returnedURL string
	if callbackURL != nil {
		returnedURL, err = callbackURL(authURL)
	} else {
		timeout := a.CallbackTimeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		opener := a.OpenBrowser
		if opener == nil {
			opener = browser.OpenURL
		}
		returnedURL, err = receiveOIDCCallback(listener, redirect.Path, authURL, opener, timeout)
	}
	if err != nil {
		return "", fmt.Errorf("%w: OIDC callback: %v", confii.ErrVaultAuth, err)
	}
	callback, err := url.Parse(returnedURL)
	if err != nil {
		return "", fmt.Errorf("%w: invalid OIDC callback URL: %v", confii.ErrVaultAuth, err)
	}
	if callback.Scheme != redirect.Scheme || callback.Host != redirect.Host || callback.Path != redirect.Path {
		return "", fmt.Errorf("%w: OIDC callback URL did not match RedirectURI", confii.ErrVaultAuth)
	}
	q := callback.Query()
	if providerErr := q.Get("error"); providerErr != "" {
		return "", fmt.Errorf("%w: OIDC provider returned %s: %s", confii.ErrVaultAuth, providerErr, q.Get("error_description"))
	}
	if q.Get("state") != expectedState || q.Get("nonce") != expectedNonce {
		return "", fmt.Errorf("%w: OIDC callback state or nonce did not match the authorization request", confii.ErrVaultAuth)
	}
	if q.Get("code") == "" {
		return "", fmt.Errorf("%w: OIDC callback omitted authorization code", confii.ErrVaultAuth)
	}

	secret, err := client.Logical().ReadWithData(fmt.Sprintf("auth/%s/oidc/callback", mp), map[string][]string{
		"state":        {q.Get("state")},
		"nonce":        {q.Get("nonce")},
		"code":         {q.Get("code")},
		"client_nonce": {clientNonce},
	})
	if err != nil {
		return "", fmt.Errorf("%w: exchange OIDC callback: %v", confii.ErrVaultAuth, err)
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return "", fmt.Errorf("%w: OIDC callback returned no auth token", confii.ErrVaultAuth)
	}
	return secret.Auth.ClientToken, nil
}

func randomOIDCNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func secretString(secret *api.Secret, key string) (string, bool) {
	if secret == nil || secret.Data == nil {
		return "", false
	}
	v, ok := secret.Data[key].(string)
	return v, ok && v != ""
}

func listenOIDCCallback(redirect *url.URL) (net.Listener, error) {
	if redirect.Scheme != "http" {
		return nil, fmt.Errorf("%w: built-in OIDC callback requires an http loopback RedirectURI", confii.ErrVaultAuth)
	}
	hostname := redirect.Hostname()
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("%w: built-in OIDC callback may listen only on loopback", confii.ErrVaultAuth)
	}
	if redirect.Port() == "" {
		return nil, fmt.Errorf("%w: built-in OIDC callback RedirectURI requires an explicit port", confii.ErrVaultAuth)
	}
	listener, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return nil, fmt.Errorf("%w: listen for OIDC callback: %v", confii.ErrVaultAuth, err)
	}
	return listener, nil
}

func receiveOIDCCallback(listener net.Listener, callbackPath, authURL string, opener func(string) error, timeout time.Duration) (string, error) {
	result := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != callbackPath {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid OIDC callback", http.StatusBadRequest)
			return
		}
		callback := *r.URL
		callback.Scheme = "http"
		callback.Host = r.Host
		callback.RawQuery = r.Form.Encode()
		select {
		case result <- callback.String():
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><title>Vault login complete</title><p>Authentication received. You may close this window.</p>"))
		default:
			http.Error(w, "OIDC callback already received", http.StatusConflict)
		}
	})
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer server.Shutdown(context.Background())

	if err := opener(authURL); err != nil {
		return "", fmt.Errorf("open browser: %w", err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case callback := <-result:
		return callback, nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return "", errors.New("OIDC callback listener closed")
		}
		return "", err
	case <-timer.C:
		return "", fmt.Errorf("timed out after %s", timeout)
	}
}
