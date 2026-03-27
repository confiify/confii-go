// Package confii is a complete configuration management library for Go.
// It loads, merges, validates, and manages configuration from YAML, JSON,
// TOML, INI, .env files, environment variables, HTTP endpoints, and cloud
// stores (AWS S3, SSM, Azure Blob, GCS, IBM COS, Git repositories) — with
// deep merging, secret resolution, source tracking, and type-safe generics.
//
// confii goes beyond configuration loading — it manages the full lifecycle:
// loading, merging with 6 per-path strategies, validating with struct tags
// and JSON Schema, resolving ${secret:key} placeholders from AWS Secrets
// Manager / Azure Key Vault / GCP Secret Manager / HashiCorp Vault (9 auth
// methods), tracking where every value came from, detecting config drift,
// versioning with rollback, and emitting observability metrics — all with
// zero global state and full thread safety via sync.RWMutex.
//
// Key features:
//
//   - Type-safe generics: Config[T] with cfg.Typed() returning *T
//   - 6 merge strategies (replace, merge, append, prepend, intersection, union) with per-path overrides
//   - ${secret:key} placeholder resolution with caching, TTL, and multi-store fallback
//   - Hydra-style config composition via _include and _defaults directives
//   - Environment resolution: automatic default + production/staging merging
//   - 4-type hook system (key, value, condition, global) for value transformation
//   - Full introspection: Explain(), Layers(), source tracking, override history
//   - Config diff, drift detection, versioning with rollback
//   - File watching with incremental reload (mtime + SHA256)
//   - Documentation generation (markdown, JSON)
//   - 10-command CLI tool
//   - Self-configuration via .confii.yaml auto-discovery
//
// Quick start:
//
//	cfg, err := confii.New[any](context.Background(),
//	    confii.WithLoaders(
//	        loader.NewYAML("config.yaml"),
//	        loader.NewEnvironment("APP"),
//	    ),
//	    confii.WithEnv("production"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	host, _ := cfg.Get("database.host")
//	port := cfg.GetIntOr("database.port", 5432)
//
// Type-safe access with generics:
//
//	cfg, err := confii.New[AppConfig](ctx,
//	    confii.WithLoaders(loader.NewYAML("config.yaml")),
//	    confii.WithValidateOnLoad(true),
//	)
//	model, _ := cfg.Typed()
//	fmt.Println(model.Database.Host) // IDE autocomplete works
//
// Builder pattern:
//
//	cfg, err := confii.NewBuilder[AppConfig]().
//	    WithEnv("production").
//	    AddLoader(loader.NewYAML("base.yaml")).
//	    AddLoader(loader.NewYAML("prod.yaml")).
//	    EnableFreezeOnLoad().
//	    Build(ctx)
//
// Secret resolution:
//
//	store := secret.NewDictStore(map[string]any{"db/password": "s3cret"})
//	resolver := secret.NewResolver(store, secret.WithCache(true))
//	cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())
//	// ${secret:db/password} in config values resolves automatically
//
// Cloud providers are opt-in via build tags: aws, azure, gcp, vault, ibm.
//
// For full documentation, examples, and the CLI tool, see
// https://github.com/confiify/confii-go
package confii
