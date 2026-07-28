// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestAzureSelfConfigSourceRegistration(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSourceProvider("azure_blob")
	if !ok {
		t.Fatal("azure_blob provider not registered")
	}
	loader, err := factory(context.Background(), map[string]any{
		"container_url": "https://account.blob.core.windows.net/config",
		"blob":          "app.yaml", "account_name": "account", "account_key": "key",
	})
	if err != nil {
		t.Fatalf("build azure source: %v", err)
	}
	azureLoader := loader.(*AzureBlobLoader)
	if azureLoader.accountName != "account" || azureLoader.accountKey != "key" {
		t.Fatalf("azure options not applied: %#v", azureLoader)
	}
	if _, err := factory(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected missing address error")
	}
	if _, err := factory(context.Background(), map[string]any{
		"container_url": "https://account.blob.core.windows.net/config", "blob": "app.yaml", "sas_token": "token",
	}); err == nil {
		t.Fatal("expected SAS account_name error")
	}
}
