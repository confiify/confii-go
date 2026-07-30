// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

// Package main shows AWS-flavoured cloud loader and secret store usage
// patterns. The example is gated behind the `aws` build tag. Cloud SDK
// versions are owned by the loader/cloud and secret/cloud modules; see
// docs/installation.md.
//
// To compile this example, from a project that imports confii-go:
//
//	cd examples/cloud
//	go build -tags aws .
//
// Sibling per-provider examples for Azure, GCP, Vault, and IBM live next to
// this file (e.g. examples/cloud/azure/main.go). Activate multiple providers by combining tags, e.g.
// `-tags "aws,vault"`.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/confiify/confii-go/loader/cloud/v2"
	secretcloud "github.com/confiify/confii-go/secret/cloud/v2"
	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/secret"
)

func main() {
	ctx := context.Background()

	// ========================================
	// Cloud Loaders (configuration sources)
	// ========================================

	// AWS S3 (this file: -tags aws)
	s3Loader, _ := cloud.NewS3("s3://my-bucket/config.yaml",
		cloud.WithS3Region("us-west-2"),
	)

	// AWS SSM Parameter Store (this file: -tags aws)
	ssmLoader := cloud.NewSSM("/myapp/production/")

	// Git (no build tag needed — uses HTTP, not a cloud SDK)
	gitLoader := cloud.NewGit(
		"https://github.com/org/config-repo", "app/config.yaml",
		cloud.WithGitBranch("main"),
		cloud.WithGitToken(os.Getenv("GIT_TOKEN")),
	)

	// ========================================
	// Cloud Secret Stores
	// ========================================

	// AWS Secrets Manager (this file: -tags aws)
	awsStore, _ := secretcloud.NewAWSSecretsManager(ctx,
		secretcloud.WithAWSRegion("us-east-1"),
	)

	// Multi-store fallback chain — extend with stores from other providers
	// by adding their build tags (e.g. `-tags "aws,vault"`).
	multi := secret.NewMultiStore([]confii.SecretStore{awsStore})

	resolver := secret.NewResolver(multi)
	cfg, err := confii.NewWithContext[any](ctx,
		confii.WithLoaders(s3Loader, ssmLoader, gitLoader),
		confii.WithMergeStrategy(confii.StrategyMerge),
		confii.WithSecretResolver(resolver),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Already resolved during New; this is an in-memory read.
	val, _ := cfg.Get("some.key")
	fmt.Println(val)
}
