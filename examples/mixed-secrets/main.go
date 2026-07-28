// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package main demonstrates environment-selected and explicitly routed
// declarative secret providers. Dict providers keep the example runnable
// without infrastructure; production projects can replace each type with
// vault, aws, azure, or gcp without changing the references.
package main

import (
	"context"
	"fmt"
	"log"

	confii "github.com/confiify/confii-go"
)

func main() {
	ctx := context.Background()
	cfg, err := confii.New[any](ctx,
		confii.WithWorkingDir("examples/mixed-secrets"),
		confii.WithEnv("production"),
	)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := cfg.GetCtx(ctx, "database.password"); err != nil {
		log.Fatal(err)
	}
	if _, err := cfg.GetCtx(ctx, "security.signing_key"); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("resolved with default %q across providers %v (values withheld)\n",
		cfg.SecretProvider(), cfg.SecretProviders())
}
