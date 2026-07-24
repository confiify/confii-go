// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build azure

// Package main demonstrates Azure Blob and Key Vault wiring. It is intended
// to be compiled with `go build -tags azure ./azure`, not run unchanged.
package main

import (
	"context"
	"log"

	confii "github.com/confiify/confii-go"
	loadercloud "github.com/confiify/confii-go/loader/cloud"
	secretcloud "github.com/confiify/confii-go/secret/cloud"
)

func main() {
	ctx := context.Background()
	blob := loadercloud.NewAzureBlob(
		"https://myaccount.blob.core.windows.net/configs",
		"production/app.yaml",
	)
	cfg, err := confii.New[any](ctx, confii.WithLoaders(blob))
	if err != nil {
		log.Fatal(err)
	}

	// nil selects Azure Default Credential (managed identity, workload
	// identity, Azure CLI, and the other standard Azure credential sources).
	store, err := secretcloud.NewAzureKeyVault("https://my-vault.vault.azure.net", nil)
	if err != nil {
		log.Fatal(err)
	}
	_, _ = store.GetSecret(ctx, "database-password")
	_, _ = cfg.Get("database.host")
}
