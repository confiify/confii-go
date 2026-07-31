// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"reflect"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestVaultSelfConfigProviderRegistered(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("vault")
	if !ok {
		t.Fatal("vault self-config secret provider was not registered")
	}
	store, err := factory(context.Background(), map[string]any{
		"address": "http://127.0.0.1:8200",
		"token":   "test-token",
	})
	if err != nil {
		t.Fatalf("build registered Vault provider: %v", err)
	}
	if store == nil {
		t.Fatal("registered Vault provider returned a nil store")
	}
}

func TestBuildVaultSelfAuthMapsOfficialAndExplicitMethods(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		typeOf any
		check  func(t *testing.T, method VaultAuthMethod)
	}{
		{
			name:   "approle",
			config: map[string]any{"role_id": "role", "secret_id_file": "/run/secret-id", "wrapping_token": true},
			typeOf: &AppRoleAuth{},
			check: func(t *testing.T, method VaultAuthMethod) {
				auth := method.(*AppRoleAuth)
				if auth.SecretIDFile != "/run/secret-id" || !auth.WrappingToken {
					t.Fatalf("AppRole mapping: %#v", auth)
				}
			},
		},
		{
			name:   "kubernetes",
			config: map[string]any{"role": "app", "token_env": "K8S_TOKEN"},
			typeOf: &KubernetesAuth{},
			check: func(t *testing.T, method VaultAuthMethod) {
				if method.(*KubernetesAuth).TokenEnv != "K8S_TOKEN" {
					t.Fatalf("Kubernetes mapping: %#v", method)
				}
			},
		},
		{name: "aws", config: map[string]any{"role": "app", "region": "us-west-2"}, typeOf: &AWSIAMAuth{}},
		{name: "aws_signed_request", config: map[string]any{"iam_http_request_method": "POST"}, typeOf: &AWSIAMSignedRequestAuth{}},
		{name: "azure", config: map[string]any{"role": "app", "resource": "audience"}, typeOf: &AzureAuth{}},
		{name: "azure_jwt", config: map[string]any{"role": "app", "jwt": "token"}, typeOf: &AzureJWTAuth{}},
		{name: "gcp", config: map[string]any{"role": "app", "auth_type": "iam", "service_account_email": "app@example.com"}, typeOf: &GCPAuth{}},
		{name: "gcp_jwt", config: map[string]any{"role": "app", "jwt": "token"}, typeOf: &JWTAuth{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			method, err := buildVaultSelfAuth(test.name, test.config, "")
			if err != nil {
				t.Fatalf("buildVaultSelfAuth: %v", err)
			}
			if reflect.TypeOf(method) != reflect.TypeOf(test.typeOf) {
				t.Fatalf("type: got %T, want %T", method, test.typeOf)
			}
			if test.check != nil {
				test.check(t, method)
			}
		})
	}
}
