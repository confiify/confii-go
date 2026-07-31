// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestOfficialVaultAuth_AppRoleRequestParity(t *testing.T) {
	f := newAuthFixture(t)
	f.handle("/v1/auth/custom-approle/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "official-approle-token")
	})

	method, err := newOfficialAppRoleMethod(&AppRoleAuth{
		RoleID:     "role-id",
		SecretID:   "secret-id",
		MountPoint: "custom-approle",
	})
	if err != nil {
		t.Fatalf("newOfficialAppRoleMethod: %v", err)
	}
	token, err := authenticateOfficialVaultMethod(context.Background(), f.client(t), method)
	if err != nil {
		t.Fatalf("authenticateOfficialVaultMethod: %v", err)
	}
	if token != "official-approle-token" {
		t.Fatalf("token: got %q, want official-approle-token", token)
	}
	if f.lastBody["role_id"] != "role-id" || f.lastBody["secret_id"] != "secret-id" {
		t.Fatalf("request body: got %#v", f.lastBody)
	}
}

func TestOfficialVaultAuth_RejectsAmbiguousCredentialSources(t *testing.T) {
	_, err := newOfficialAppRoleMethod(&AppRoleAuth{RoleID: "role", SecretID: "inline", SecretIDEnv: "SECRET_ID"})
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Fatalf("AppRole error: got %v, want ErrVaultAuth", err)
	}
	_, err = newOfficialKubernetesMethod(&KubernetesAuth{Role: "role", JWT: "inline", TokenPath: "/token"})
	if !errors.Is(err, confii.ErrVaultAuth) {
		t.Fatalf("Kubernetes error: got %v, want ErrVaultAuth", err)
	}
}

func TestOfficialVaultAuth_AppRoleReadsRotatedSecretIDFile(t *testing.T) {
	dir := t.TempDir()
	secretIDPath := filepath.Join(dir, "secret-id")
	if err := os.WriteFile(secretIDPath, []byte("first\n"), 0o600); err != nil {
		t.Fatalf("write first SecretID: %v", err)
	}

	f := newAuthFixture(t)
	f.handle("/v1/auth/approle/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "official-approle-token")
	})
	auth := &AppRoleAuth{RoleID: "role-id", SecretIDFile: secretIDPath}
	for _, secretID := range []string{"first", "second"} {
		if secretID == "second" {
			if err := os.WriteFile(secretIDPath, []byte(secretID+"\n"), 0o600); err != nil {
				t.Fatalf("rotate SecretID: %v", err)
			}
		}
		method, err := newOfficialAppRoleMethod(auth)
		if err != nil {
			t.Fatalf("newOfficialAppRoleMethod: %v", err)
		}
		if _, err := authenticateOfficialVaultMethod(context.Background(), f.client(t), method); err != nil {
			t.Fatalf("authenticateOfficialVaultMethod: %v", err)
		}
		if f.lastBody["secret_id"] != secretID {
			t.Fatalf("secret_id: got %v, want %q", f.lastBody["secret_id"], secretID)
		}
	}
}

func TestOfficialVaultAuth_KubernetesRequestParity(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("projected-token"), 0o600); err != nil {
		t.Fatalf("write service-account token: %v", err)
	}

	f := newAuthFixture(t)
	f.handle("/v1/auth/custom-kubernetes/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "official-kubernetes-token")
	})
	method, err := newOfficialKubernetesMethod(&KubernetesAuth{
		Role:       "confii",
		TokenPath:  tokenPath,
		MountPoint: "custom-kubernetes",
	})
	if err != nil {
		t.Fatalf("newOfficialKubernetesMethod: %v", err)
	}
	if _, err := authenticateOfficialVaultMethod(context.Background(), f.client(t), method); err != nil {
		t.Fatalf("authenticateOfficialVaultMethod: %v", err)
	}
	if f.lastBody["role"] != "confii" || f.lastBody["jwt"] != "projected-token" {
		t.Fatalf("request body: got %#v", f.lastBody)
	}
}

func TestOfficialVaultAuth_AWSBuildsSignedRequest(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret-example")
	t.Setenv("AWS_SESSION_TOKEN", "session-example")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")

	f := newAuthFixture(t)
	f.handle("/v1/auth/custom-aws/login", func(w http.ResponseWriter, _ *http.Request) {
		writeAuthOK(w, "official-aws-token")
	})
	method, err := newOfficialAWSMethod(&AWSIAMAuth{
		Role:       "confii",
		Region:     "us-west-2",
		MountPoint: "custom-aws",
	})
	if err != nil {
		t.Fatalf("newOfficialAWSMethod: %v", err)
	}
	if _, err := authenticateOfficialVaultMethod(context.Background(), f.client(t), method); err != nil {
		t.Fatalf("authenticateOfficialVaultMethod: %v", err)
	}
	for _, key := range []string{"iam_http_request_method", "iam_request_url", "iam_request_body", "iam_request_headers"} {
		if f.lastBody[key] == nil || f.lastBody[key] == "" {
			t.Errorf("request body %s is empty: %#v", key, f.lastBody)
		}
	}
	if f.lastBody["role"] != "confii" {
		t.Errorf("role: got %v, want confii", f.lastBody["role"])
	}
}

func TestOfficialVaultAuth_ProviderConstructors(t *testing.T) {
	if _, err := newOfficialAzureMethod(&AzureAuth{Role: "confii"}); err != nil {
		t.Fatalf("newOfficialAzureMethod: %v", err)
	}
	if _, err := newOfficialGCPMethod(&GCPAuth{Role: "confii", AuthType: "gce"}); err != nil {
		t.Fatalf("newOfficialGCPMethod gce: %v", err)
	}
	if _, err := newOfficialGCPMethod(&GCPAuth{Role: "confii", AuthType: "iam", ServiceAccountEmail: "confii@example.iam.gserviceaccount.com"}); err != nil {
		t.Fatalf("newOfficialGCPMethod iam: %v", err)
	}
}
