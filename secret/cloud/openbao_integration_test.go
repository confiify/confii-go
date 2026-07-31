// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestOpenBaoInterop(t *testing.T) {
	address := os.Getenv("CONFII_OPENBAO_ADDR")
	token := os.Getenv("CONFII_OPENBAO_TOKEN")
	roleID := os.Getenv("CONFII_OPENBAO_ROLE_ID")
	secretID := os.Getenv("CONFII_OPENBAO_SECRET_ID")
	if address == "" || token == "" || roleID == "" || secretID == "" {
		t.Skip("real OpenBao endpoint and ephemeral test credentials are not configured")
	}

	ctx := context.Background()
	const key = "confii-ci/runtime"

	store, err := NewOpenBao(WithVaultURL(address),
		WithVaultToken(token),
		WithVaultMountPoint("secret"),
		WithVaultKVVersion(2),
	)
	if err != nil {
		t.Fatalf("NewOpenBao token store: %v", err)
	}
	if err := store.SetSecret(ctx, key, map[string]any{
		"source": "openbao",
		"value":  "live-provider-test",
	}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteSecret(ctx, key) })

	got, err := store.GetSecret(ctx, key, confii.WithField("source"))
	if err != nil {
		t.Fatalf("GetSecret field: %v", err)
	}
	if got != "openbao" {
		t.Fatalf("GetSecret field = %#v, want %q", got, "openbao")
	}
	keys, err := store.ListSecrets(ctx, "confii-ci")
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if !slices.Contains(keys, "runtime") {
		t.Fatalf("ListSecrets = %#v, want runtime", keys)
	}

	appRoleStore, err := NewOpenBao(WithVaultURL(address),
		WithVaultAuth(&AppRoleAuth{RoleID: roleID, SecretID: secretID}),
	)
	if err != nil {
		t.Fatalf("NewOpenBao AppRole store: %v", err)
	}
	got, err = appRoleStore.GetSecret(ctx, key, confii.WithField("value"))
	if err != nil {
		t.Fatalf("AppRole GetSecret: %v", err)
	}
	if got != "live-provider-test" {
		t.Fatalf("AppRole GetSecret = %#v, want %q", got, "live-provider-test")
	}

	if err := store.DeleteSecret(ctx, key); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if _, err := store.GetSecret(ctx, key); !errors.Is(err, confii.ErrSecretNotFound) {
		t.Fatalf("GetSecret after delete = %v, want ErrSecretNotFound", err)
	}
}
