// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

// Package main demonstrates Vault KV plus interactive OIDC authentication.
package main

import (
	"context"
	"log"

	secretcloud "github.com/confiify/confii-go/secret/cloud/v2"
	confii "github.com/confiify/confii-go/v2"
)

func main() {
	ctx := context.Background()
	store, err := secretcloud.NewHashiCorpVault(
		secretcloud.WithVaultURL("https://vault.example.com:8200"),
		secretcloud.WithVaultMountPoint("secret"),
		secretcloud.WithVaultKVVersion(2),
		secretcloud.WithVaultAuth(&secretcloud.OIDCAuth{
			Role:        "confii-developers",
			MountPoint:  "oidc",
			RedirectURI: "http://localhost:8250/oidc/callback",
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := store.GetSecret(ctx, "apps/confii/database", confii.WithField("password")); err != nil {
		log.Fatal(err)
	}
}
