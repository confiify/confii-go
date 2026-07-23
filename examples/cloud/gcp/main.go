//go:build gcp

// Package main demonstrates Google Cloud Storage and Secret Manager wiring.
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
	gcs := loadercloud.NewGCS("my-config-bucket", "production/app.yaml",
		loadercloud.WithGCSProject("my-quota-project"),
	)
	cfg, err := confii.New[any](ctx, confii.WithLoaders(gcs))
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
