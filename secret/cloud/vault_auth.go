// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

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
	"strings"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/hashicorp/vault/api"
	"github.com/pkg/browser"
)

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
func (a *TokenAuth) Authenticate(ctx context.Context, _ *api.Client) (string, error) {
	if err := vaultAuthContextError(ctx); err != nil {
		return "", err
	}
	if a.Token == "" {
		return "", fmt.Errorf("%w: TokenAuth.Token is empty", confii.ErrVaultAuth)
	}
	return a.Token, nil
}

// AppRoleAuth authenticates via AppRole.
//
// RoleID and exactly one SecretID source are required. MountPoint defaults to
// "approle". The login is performed against `auth/<MountPoint>/login`.
type AppRoleAuth struct {
	// RoleID identifies the AppRole. Required.
	RoleID string
	// SecretID is an inline secret credential bound to the role.
	SecretID string
	// SecretIDFile reads the SecretID at login time from a file. Use exactly
	// one of SecretID, SecretIDFile, or SecretIDEnv.
	SecretIDFile string
	// SecretIDEnv reads the SecretID at login time from an environment variable.
	SecretIDEnv string
	// WrappingToken treats the selected SecretID source as a response-wrapping
	// token and asks Vault to unwrap it before login.
	WrappingToken bool
	// MountPoint overrides the auth method mount path (default "approle").
	MountPoint string
}

// Authenticate logs in to Vault using the AppRole auth method with the configured role ID and secret ID.
func (a *AppRoleAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	method, err := newOfficialAppRoleMethod(a)
	if err != nil {
		return "", err
	}
	return authenticateOfficialVaultMethod(ctx, client, method)
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
	PasswordProvider func(context.Context) (string, error)
	// MountPoint overrides the auth method mount path (default "ldap").
	MountPoint string
}

// Authenticate logs in to Vault using LDAP credentials, optionally obtaining the password from a provider function.
func (a *LDAPAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	if err := vaultAuthContextError(ctx); err != nil {
		return "", err
	}
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
		password, err = a.PasswordProvider(ctx)
		if err != nil {
			return "", fmt.Errorf("%w: ldap password provider: %w", confii.ErrVaultAuth, err)
		}
	}
	if password == "" {
		return "", fmt.Errorf("%w: LDAPAuth requires Password or PasswordProvider", confii.ErrVaultAuth)
	}
	secret, err := client.Logical().WriteWithContext(ctx, fmt.Sprintf("auth/%s/login/%s", mp, a.Username), map[string]any{
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
// or comparable bearer JWT. This auth method does not mint the JWT.
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
func (a *JWTAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	if err := vaultAuthContextError(ctx); err != nil {
		return "", err
	}
	if a.Role == "" || a.JWT == "" {
		return "", fmt.Errorf("%w: JWTAuth requires Role and JWT", confii.ErrVaultAuth)
	}
	mp := a.MountPoint
	if mp == "" {
		mp = "jwt"
	}
	secret, err := client.Logical().WriteWithContext(ctx, fmt.Sprintf("auth/%s/login", mp), map[string]any{
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
// Role is required. Select at most one inline JWT, token file, or environment
// source. With none selected, Authenticate reads the token from the canonical
// in-pod path
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
	// JWT is an inline service-account JWT.
	JWT string
	// TokenPath reads the projected service-account token from a file.
	TokenPath string
	// TokenEnv reads the service-account token from an environment variable.
	// Use at most one of JWT, TokenPath, or TokenEnv.
	TokenEnv string
	// MountPoint overrides the auth method mount path (default "kubernetes").
	MountPoint string
}

// Authenticate logs in to Vault using a Kubernetes service-account token and
// configured role. The selected file or environment source is read on each
// invocation so rotated credentials are observed.
func (a *KubernetesAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	method, err := newOfficialKubernetesMethod(a)
	if err != nil {
		return "", err
	}
	return authenticateOfficialVaultMethod(ctx, client, method)
}

// AWSIAMAuth authenticates through Vault's official AWS IAM auth module. The
// module discovers credentials from the standard AWS environment, shared
// credentials file, or instance metadata, signs STS GetCallerIdentity, and
// submits the Vault login request. Role may be empty when Vault can infer it
// from the IAM principal.
type AWSIAMAuth struct {
	// Role is the optional Vault AWS role to log in against.
	Role string
	// IAMServerIDHeader is the optional X-Vault-AWS-IAM-Server-ID header
	// value that must match the role's bound_iam_server_id_header_value.
	IAMServerIDHeader string
	// Region selects the AWS region used to sign the STS request. The official
	// Vault client defaults to us-east-1.
	Region string
	// MountPoint overrides the auth method mount path (default "aws").
	MountPoint string
}

// Authenticate discovers AWS credentials, signs the IAM request, and logs in
// through the official Vault AWS auth module.
func (a *AWSIAMAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	method, err := newOfficialAWSMethod(a)
	if err != nil {
		return "", err
	}
	return authenticateOfficialVaultMethod(ctx, client, method)
}

// AzureAuth authenticates through Vault's official Azure auth module. It
// obtains the managed-identity token and instance metadata from Azure IMDS,
// then submits them to the configured Vault auth mount.
type AzureAuth struct {
	// Role is the Vault Azure role to log in against. Required.
	Role string
	// Resource overrides the Azure resource/audience requested from IMDS.
	Resource string
	// MountPoint overrides the auth method mount path (default "azure").
	MountPoint string
}

// Authenticate obtains Azure managed-identity credentials and logs in through
// the official Vault Azure auth module.
func (a *AzureAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	method, err := newOfficialAzureMethod(a)
	if err != nil {
		return "", err
	}
	return authenticateOfficialVaultMethod(ctx, client, method)
}

// GCPAuth authenticates through Vault's official GCP auth module. GCE mode
// obtains an identity JWT from the metadata service. IAM mode signs a JWT
// through IAM Credentials using application default credentials.
type GCPAuth struct {
	// Role is the Vault GCP role to log in against. Required.
	Role string
	// AuthType selects gce (default) or iam for the official Vault GCP auth
	// implementation.
	AuthType string
	// ServiceAccountEmail is required when AuthType is iam.
	ServiceAccountEmail string
	// MountPoint overrides the auth method mount path (default "gcp").
	MountPoint string
}

// Authenticate discovers or signs the GCP identity and logs in through the
// official Vault GCP auth module.
func (a *GCPAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	method, err := newOfficialGCPMethod(a)
	if err != nil {
		return "", err
	}
	return authenticateOfficialVaultMethod(ctx, client, method)
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
	CallbackProvider func(ctx context.Context, authorizationURL string) (callbackURL string, err error)
}

// Authenticate completes Vault's OIDC auth_url and callback protocol.
func (a *OIDCAuth) Authenticate(ctx context.Context, client *api.Client) (string, error) {
	if err := vaultAuthContextError(ctx); err != nil {
		return "", err
	}
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
			return "", fmt.Errorf("%w: generate OIDC client nonce: %w", confii.ErrVaultAuth, err)
		}
	}

	var callbackURL func(context.Context, string) (string, error)
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
	authURLSecret, err := client.Logical().WriteWithContext(ctx, fmt.Sprintf("auth/%s/oidc/auth_url", mp), payload)
	if err != nil {
		return "", fmt.Errorf("%w: request OIDC authorization URL: %w", confii.ErrVaultAuth, err)
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
		returnedURL, err = callbackURL(ctx, authURL)
	} else {
		timeout := a.CallbackTimeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		opener := a.OpenBrowser
		if opener == nil {
			opener = browser.OpenURL
		}
		returnedURL, err = receiveOIDCCallback(ctx, listener, redirect.Path, authURL, opener, timeout)
	}
	if err != nil {
		return "", fmt.Errorf("%w: OIDC callback: %w", confii.ErrVaultAuth, err)
	}
	callback, err := url.Parse(returnedURL)
	if err != nil {
		return "", fmt.Errorf("%w: invalid OIDC callback URL: %w", confii.ErrVaultAuth, err)
	}
	if callback.Scheme != redirect.Scheme || callback.Host != redirect.Host || callback.Path != redirect.Path {
		return "", fmt.Errorf("%w: OIDC callback URL did not match RedirectURI", confii.ErrVaultAuth)
	}
	q := callback.Query()
	if providerErr := q.Get("error"); providerErr != "" {
		return "", fmt.Errorf("%w: OIDC provider returned %s: %s", confii.ErrVaultAuth, providerErr, q.Get("error_description"))
	}
	if q.Get("state") != expectedState {
		return "", fmt.Errorf("%w: OIDC callback state did not match the authorization request", confii.ErrVaultAuth)
	}
	if q.Get("code") == "" {
		return "", fmt.Errorf("%w: OIDC callback omitted authorization code", confii.ErrVaultAuth)
	}

	secret, err := client.Logical().ReadWithDataWithContext(ctx, fmt.Sprintf("auth/%s/oidc/callback", mp), map[string][]string{
		"state": {q.Get("state")},
		// OIDC providers return state and code to the redirect URI. The nonce
		// is retained from Vault's auth_url response and supplied during the
		// callback exchange; it is validated against the ID token by Vault.
		"nonce":        {expectedNonce},
		"code":         {q.Get("code")},
		"client_nonce": {clientNonce},
	})
	if err != nil {
		return "", fmt.Errorf("%w: exchange OIDC callback: %w", confii.ErrVaultAuth, err)
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
		return nil, fmt.Errorf("%w: listen for OIDC callback: %w", confii.ErrVaultAuth, err)
	}
	return listener, nil
}

func receiveOIDCCallback(ctx context.Context, listener net.Listener, callbackPath, authURL string, opener func(string) error, timeout time.Duration) (string, error) {
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
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

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
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func vaultAuthContextError(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", confii.ErrVaultAuth)
	}
	return ctx.Err()
}
