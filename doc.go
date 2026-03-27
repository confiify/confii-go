// Package confii provides a unified interface for loading, merging,
// validating, and accessing configuration values from heterogeneous sources.
//
// It supports file-based formats (YAML, JSON, TOML, INI, .env), environment
// variables, remote stores (HTTP, S3, SSM, Azure Blob, GCS, IBM COS, Git),
// and secret management providers (AWS Secrets Manager, Azure Key Vault,
// GCP Secret Manager, HashiCorp Vault).
//
// Basic usage:
//
//	cfg, err := confii.New[AppConfig](
//	    confii.WithLoaders(loader.NewYAML("config.yaml")),
//	    confii.WithEnv("production"),
//	    confii.WithValidateOnLoad(true),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Untyped access
//	host, err := cfg.Get("database.host")
//
//	// Typed access
//	model, err := cfg.Typed()
//	fmt.Println(model.Database.Host)
package confii
