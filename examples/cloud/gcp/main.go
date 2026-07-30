// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build gcp

// Package main demonstrates Google Cloud Storage and Secret Manager wiring.
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
	gcs := loadercloud.NewGCS("my-config-bucket", "production/app.yaml",
		loadercloud.WithGCSProject("my-quota-project"),
	)
	cfg, err := confii.NewWithContext[any](ctx, confii.WithLoaders(gcs))
	if err != nil {
		log.Fatal(err)
	}
	store, err := secretcloud.NewGCPSecretManager(ctx, "my-project")
	if err != nil {
		log.Fatal(err)
	}
	_, _ = store.GetSecret(ctx, "database-password")
	_, _ = cfg.Get("database.host")
}
