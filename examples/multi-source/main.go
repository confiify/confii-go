// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates loading configuration from multiple sources.
// Later loaders override earlier ones. Environment variables can also be used.
package main

import (
	"fmt"
	"log"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

func main() {
	cfg, err := confii.New[any](confii.WithLoaders(
		loader.NewYAML("defaults.yaml"),  // base config
		loader.NewYAML("overrides.yaml"), // overrides base values
		loader.NewEnvironment("APP"),     // APP_DATABASE__HOST overrides further
	),
		confii.WithMergeStrategy(confii.StrategyMerge),
	)
	if err != nil {
		log.Fatal(err)
	}

	host := cfg.MustGet("database.host")
	ssl := cfg.MustGet("database.ssl")
	ttl := cfg.MustGet("cache.ttl")

	fmt.Println("Host:", host) // prod-db.example.com (from overrides.yaml)
	fmt.Println("SSL:", ssl)   // true (added by overrides.yaml)
	fmt.Println("TTL:", ttl)   // 3600 (overridden by overrides.yaml)
}
