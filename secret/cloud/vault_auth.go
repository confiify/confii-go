//go:build vault

package cloud

import (
	"fmt"

	"github.com/hashicorp/vault/api"
)

// TokenAuth authenticates with a static token.
type TokenAuth struct {
	Token string
}

func (a *TokenAuth) Authenticate(client *api.Client) (string, error) {
	return a.Token, nil
}

// AppRoleAuth authenticates via AppRole.
type AppRoleAuth struct {
	RoleID     string
	SecretID   string
	MountPoint string
}

func (a *AppRoleAuth) Authenticate(client *api.Client) (string, error) {
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
	return secret.Auth.ClientToken, nil
}

// LDAPAuth authenticates via LDAP.
type LDAPAuth struct {
	Username         string
	Password         string
	PasswordProvider func() (string, error)
	MountPoint       string
}

func (a *LDAPAuth) Authenticate(client *api.Client) (string, error) {
	mp := a.MountPoint
	if mp == "" {
		mp = "ldap"
	}
	password := a.Password
	if password == "" && a.PasswordProvider != nil {
		var err error
		password, err = a.PasswordProvider()
		if err != nil {
			return "", fmt.Errorf("password provider: %w", err)
		}
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login/%s", mp, a.Username), map[string]any{
		"password": password,
	})
	if err != nil {
		return "", err
	}
	return secret.Auth.ClientToken, nil
}

// JWTAuth authenticates via JWT.
type JWTAuth struct {
	Role       string
	JWT        string
	MountPoint string
}

func (a *JWTAuth) Authenticate(client *api.Client) (string, error) {
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
	return secret.Auth.ClientToken, nil
}

// KubernetesAuth authenticates via Kubernetes service account.
type KubernetesAuth struct {
	Role       string
	JWT        string
	MountPoint string
}

func (a *KubernetesAuth) Authenticate(client *api.Client) (string, error) {
	mp := a.MountPoint
	if mp == "" {
		mp = "kubernetes"
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), map[string]any{
		"role": a.Role,
		"jwt":  a.JWT,
	})
	if err != nil {
		return "", err
	}
	return secret.Auth.ClientToken, nil
}

// AWSIAMAuth authenticates via AWS IAM.
type AWSIAMAuth struct {
	Role       string
	MountPoint string
}

func (a *AWSIAMAuth) Authenticate(client *api.Client) (string, error) {
	mp := a.MountPoint
	if mp == "" {
		mp = "aws"
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), map[string]any{
		"role": a.Role,
	})
	if err != nil {
		return "", err
	}
	return secret.Auth.ClientToken, nil
}

// AzureAuth authenticates via Azure Managed Identity.
type AzureAuth struct {
	Role       string
	Resource   string
	MountPoint string
}

func (a *AzureAuth) Authenticate(client *api.Client) (string, error) {
	mp := a.MountPoint
	if mp == "" {
		mp = "azure"
	}
	data := map[string]any{"role": a.Role}
	if a.Resource != "" {
		data["resource"] = a.Resource
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), data)
	if err != nil {
		return "", err
	}
	return secret.Auth.ClientToken, nil
}

// GCPAuth authenticates via GCP IAM or GCE metadata.
type GCPAuth struct {
	Role       string
	JWT        string
	MountPoint string
}

func (a *GCPAuth) Authenticate(client *api.Client) (string, error) {
	mp := a.MountPoint
	if mp == "" {
		mp = "gcp"
	}
	data := map[string]any{"role": a.Role}
	if a.JWT != "" {
		data["jwt"] = a.JWT
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), data)
	if err != nil {
		return "", err
	}
	return secret.Auth.ClientToken, nil
}

// OIDCAuth authenticates via OpenID Connect.
type OIDCAuth struct {
	Role       string
	MountPoint string
}

func (a *OIDCAuth) Authenticate(client *api.Client) (string, error) {
	mp := a.MountPoint
	if mp == "" {
		mp = "oidc"
	}
	secret, err := client.Logical().Write(fmt.Sprintf("auth/%s/login", mp), map[string]any{
		"role": a.Role,
	})
	if err != nil {
		return "", err
	}
	return secret.Auth.ClientToken, nil
}
