// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package confii loads, composes, validates, and manages application
// configuration. Sources include local configuration files, environment
// variables, HTTP endpoints, and optional cloud integrations. Configuration
// can be accessed as scalar values, dictionaries, or typed Go structs.
//
// Key features:
//
//   - Type-safe generics: Config[T] with cfg.Typed returning *T
//   - Configurable merge strategies with per-path overrides
//   - ${secret:key} placeholder resolution with caching, TTL, and multi-store fallback
//   - Configuration composition via _include and _defaults directives
//   - Environment resolution: automatic default + production/staging merging
//   - Key, value, condition, and global transformation hooks
//   - Full introspection: Explain, Layers, source tracking, override history
//   - Config diff, drift detection, versioning with rollback
//   - File watching with incremental reload (mtime + SHA256)
//   - Documentation generation (markdown, JSON)
//   - CLI commands for initialization, inspection, validation, and migration
//   - Self-configuration via .confii.yaml, .confii.json, or .confii.toml
//
// Quick start:
//
//	cfg, err := confii.New[any](
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
//	cfg, err := confii.NewWithContext[AppConfig](ctx,
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
//	    BuildWithContext(ctx)
//
// Secret resolution:
//
//	store := secret.NewDictStore(map[string]any{"db/password": "s3cret"})
//	resolver := secret.NewResolver(store, secret.WithCache(true))
//	cfg, err := confii.New[AppConfig](confii.WithSecretResolver(resolver))
//	// ${secret:db/password} in config values resolves automatically
//
// Cloud providers are opt-in via build tags: aws, azure, gcp, vault, ibm.
//
// # Hook pipeline contract
//
// Hooks run while a candidate snapshot is materialized: environment expansion,
// type casting, secret resolution, and custom hooks complete before validation
// and publication. [Config.Get], [Config.Typed], [Config.ToDict], and
// [Config.Export] then read the same immutable effective values without rerunning
// transformations. Register hooks at construction with [WithGlobalHook],
// [WithKeyHook], [WithValueHook], or [WithConditionHook].
//
// For full documentation, examples, and the CLI tool, see
// https://github.com/confiify/confii-go
package confii
