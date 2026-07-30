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

	confii "github.com/confiify/confii-go/v2"
)

func main() {
	ctx := context.Background()
	cfg, err := confii.NewWithContext[any](ctx,
		confii.WithWorkingDir("examples/mixed-secrets"),
		confii.WithEnv("production"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("initialized with default %q across providers %v; all effective secrets are ready (values withheld)\n",
		cfg.SecretProvider(), cfg.SecretProviders())
}
