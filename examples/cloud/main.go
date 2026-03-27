// Package main shows cloud loader and secret store usage patterns.
// These require build tags: go build -tags "aws,azure,gcp,vault,ibm"
//
// This example is illustrative — it won't compile without cloud dependencies
// and valid credentials. See each section for the required build tag.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader/cloud"
	secretcloud "github.com/qualitycoe/confii-go/secret/cloud"
	"github.com/qualitycoe/confii-go/secret"
)

func main() {
	ctx := context.Background()

	// ========================================
	// Cloud Loaders (configuration sources)
	// ========================================

	// AWS S3 (requires: go build -tags aws)
	s3Loader, _ := cloud.NewS3("s3://my-bucket/config.yaml",
		cloud.WithS3Region("us-west-2"),
	)

	// AWS SSM Parameter Store (requires: go build -tags aws)
	ssmLoader := cloud.NewSSM("/myapp/production/")

	// Azure Blob Storage (requires: go build -tags azure)
	azLoader := cloud.NewAzureBlob("my-container", "config.yaml")

	// Google Cloud Storage (requires: go build -tags gcp)
	gcsLoader := cloud.NewGCS("my-bucket", "config.yaml")

	// Git (no build tag needed)
	gitLoader := cloud.NewGit(
		"https://github.com/org/config-repo", "app/config.yaml",
		cloud.WithGitBranch("main"),
		cloud.WithGitToken(os.Getenv("GIT_TOKEN")),
	)

	// Use any combination of loaders
	cfg, err := confii.New[any](ctx,
		confii.WithLoaders(s3Loader, ssmLoader, azLoader, gcsLoader, gitLoader),
		confii.WithDeepMerge(true),
	)
	if err != nil {
		log.Fatal(err)
	}

	// ========================================
	// Cloud Secret Stores
	// ========================================

	// AWS Secrets Manager (requires: go build -tags aws)
	awsStore, _ := secretcloud.NewAWSSecretsManager(ctx,
		secretcloud.WithAWSRegion("us-east-1"),
	)

	// HashiCorp Vault (requires: go build -tags vault)
	vaultStore, _ := secretcloud.NewHashiCorpVault(
		secretcloud.WithVaultURL("https://vault.example.com"),
		secretcloud.WithVaultAuth(&secretcloud.AppRoleAuth{
			RoleID:   "role-id",
			SecretID: "secret-id",
		}),
		secretcloud.WithVaultMountPoint("secret"),
	)

	// Azure Key Vault (requires: go build -tags azure)
	azStore, _ := secretcloud.NewAzureKeyVault(
		"https://my-vault.vault.azure.net", nil,
	)

	// GCP Secret Manager (requires: go build -tags gcp)
	gcpStore, _ := secretcloud.NewGCPSecretManager(ctx, "my-project-id")

	// Multi-store fallback chain
	multi := secret.NewMultiStore([]confii.SecretStore{
		awsStore, vaultStore, azStore, gcpStore,
	})

	// Wire up with config
	resolver := secret.NewResolver(multi)
	cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())

	val, _ := cfg.Get("some.key")
	fmt.Println(val)
}
