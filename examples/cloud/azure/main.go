// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

// Package main demonstrates Azure Blob and Key Vault wiring. It is intended
// to be compiled with `go build -tags azure ./azure`, not run unchanged.
package main

import (
	"context"
	"log"

	loadercloud "github.com/confiify/confii-go/loader/cloud/v2"
	secretcloud "github.com/confiify/confii-go/secret/cloud/v2"
	confii "github.com/confiify/confii-go/v2"
)

func main() {
	ctx := context.Background()
	blob := loadercloud.NewAzureBlob(
		"https://myaccount.blob.core.windows.net/configs",
		"production/app.yaml",
	)
	cfg, err := confii.NewWithContext[any](ctx, confii.WithLoaders(blob))
	if err != nil {
		log.Fatal(err)
	}

	// nil selects Azure Default Credential (managed identity, workload
	// identity, Azure CLI, and the other standard Azure credential sources).
	store, err := secretcloud.NewAzureKeyVault("https://my-vault.vault.azure.net", nil)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := store.GetSecret(ctx, "database-password"); err != nil {
		log.Fatal(err)
	}
	if _, err := cfg.Get("database.host"); err != nil {
		log.Fatal(err)
	}
}
