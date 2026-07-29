// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates secret management with Confii.
// Secrets are resolved via ${secret:key} placeholders in config values.
// The resolver is configured with a "prod/" prefix, so each ${secret:db/password}
// placeholder in config is looked up as "prod/db/password" in the store.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/confiify/confii-go/secret"
)

func main() {
	// Create an in-memory secret store (use cloud stores in production).
	// Keys are seeded with the "prod/" prefix to match WithResolverPrefix
	// below — the resolver prepends that prefix to every lookup, so the
	// in-store keys must already include it.
	store := secret.NewDictStore(map[string]any{
		"prod/db/password": "s3cret",
		"prod/api/key":     "abc123",
	})

	// Create a resolver with caching
	resolver := secret.NewResolver(store,
		secret.WithCache(true),
		secret.WithCacheTTL(5*time.Minute),
		secret.WithResolverPrefix("prod/"),
	)

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("config.yaml")),
		confii.WithSecretResolver(resolver),
	)
	if err != nil {
		log.Fatal(err)
	}

	// New has already resolved every effective ${secret:...} reference.
	// Supports formats:
	//   ${secret:key}
	//   ${secret:key:json_path}
	//   ${secret:key:json_path:version}

	// Read the ready in-memory value without writing it to logs or stdout.
	// Applications pass the returned value directly to the component
	// that needs it (for example, a database client).
	if _, err := cfg.Get("database.password"); err != nil {
		log.Fatal(err) // no provider traffic occurs on this ordinary read
	}
	fmt.Println("Database password resolved successfully")

	// Check cache stats
	fmt.Println("Cache stats:", resolver.CacheStats())
}
