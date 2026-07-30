// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws || azure || gcp || vault

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go/v2"
)

func TestSelfConfigStoreAdapterForwardsDeclarativeVersion(t *testing.T) {
	var gotKey, gotVersion string
	adapter := selfConfigStoreAdapter{get: func(_ context.Context, key string, opts ...confii.SecretOption) (any, error) {
		gotKey = key
		gotVersion = confii.ResolveSecretOptions(opts...).Version
		return "resolved", nil
	}}

	got, err := adapter.ReadSecret(context.Background(), confii.SecretRequest{
		Key:     "service/credential",
		Version: "v2",
	})
	if err != nil || got != "resolved" || gotKey != "service/credential" || gotVersion != "v2" {
		t.Fatalf("versioned request = value %v key %q version %q err %v", got, gotKey, gotVersion, err)
	}

	got, err = adapter.ReadSecret(context.Background(), confii.SecretRequest{Key: "plain"})
	if err != nil || got != "resolved" || gotKey != "plain" || gotVersion != "" {
		t.Fatalf("plain request = value %v key %q version %q err %v", got, gotKey, gotVersion, err)
	}
}

func TestSelfConfigStoreAdapterExtractsDeclarativeField(t *testing.T) {
	adapter := selfConfigStoreAdapter{get: func(context.Context, string, ...confii.SecretOption) (any, error) {
		return `{"credentials":{"password":"cloud-secret"}}`, nil
	}}

	got, err := adapter.ReadSecret(context.Background(), confii.SecretRequest{
		Key:   "service",
		Field: "credentials.password",
	})
	if err != nil || got != "cloud-secret" {
		t.Fatalf("declarative field = %v, %v", got, err)
	}
}
