// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws || azure || gcp || vault

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestSelfConfigStoreAdapterForwardsDeclarativeVersion(t *testing.T) {
	var gotKey, gotVersion string
	adapter := selfConfigStoreAdapter{get: func(_ context.Context, key string, opts ...confii.SecretOption) (any, error) {
		gotKey = key
		gotVersion = confii.ResolveSecretOptions(opts...).Version
		return "resolved", nil
	}}

	got, err := adapter.GetSecretRequest(context.Background(), confii.SelfConfigSecretRequest{
		Key:     "service/credential",
		Version: "v2",
	})
	if err != nil || got != "resolved" || gotKey != "service/credential" || gotVersion != "v2" {
		t.Fatalf("versioned request = value %v key %q version %q err %v", got, gotKey, gotVersion, err)
	}

	got, err = adapter.GetSecret(context.Background(), "plain")
	if err != nil || got != "resolved" || gotKey != "plain" || gotVersion != "" {
		t.Fatalf("legacy request = value %v key %q version %q err %v", got, gotKey, gotVersion, err)
	}
}
