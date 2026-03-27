<p align="center">
  <img src="art/confii-go.png" alt="Confii Logo" />
</p>

# Confii

A powerful, type-safe configuration management library for Go. Load, merge, validate, and access configuration from multiple sources with a clean, idiomatic API.

[![Go Reference](https://pkg.go.dev/badge/github.com/confiify/confii-go.svg)](https://pkg.go.dev/github.com/confiify/confii-go)

## Features

- **Multi-source loading** — YAML, JSON, TOML, INI, .env files, environment variables, HTTP endpoints, and cloud stores (S3, SSM, Azure Blob, GCS, IBM COS, Git)
- **Configuration composition** — `_include` and `_defaults` directives with cycle detection for Hydra-style config composition
- **Type-safe access** — Go generics (`Config[T]`) with struct tag validation via `mapstructure` + `go-playground/validator`
- **Deep merging** — 6 merge strategies (replace, merge, append, prepend, intersection, union) with per-path overrides
- **Environment resolution** — Automatic `default` + environment-specific section merging
- **Secret management** — `${secret:key}` placeholder resolution with pluggable stores (AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, HashiCorp Vault with 9 auth methods, env vars)
- **Hook system** — 4 hook types (key, value, condition, global) for value transformation on access
- **Source tracking** — Know exactly where each config value came from, full override history, conflict detection
- **Dynamic reloading** — File watching via fsnotify, incremental reload (only changed files), dry-run mode
- **Observability** — Access metrics, event emission, config versioning with rollback
- **Diff & drift detection** — Compare configurations, detect unintended changes, diff version snapshots
- **Introspection** — `Explain()`, `Schema()`, `Layers()`, debug reports, documentation generation
- **CLI tool** — 10 commands: load, get, validate, export, diff, debug, explain, lint, docs, migrate
- **Self-configuration** — `.confii.yaml` auto-discovery with 3-tier priority (explicit > self-config > default)
- **Thread-safe** — `sync.RWMutex` protection, safe for concurrent reads

## Why Confii?

Go has several configuration libraries, but none provides a complete configuration *management* solution. Most handle loading and reading — Confii handles the full lifecycle: loading, merging, validating, secret resolution, drift detection, versioning, and observability.

### Feature Comparison

| Feature | Confii | Viper | Koanf | envconfig | cleanenv | konfig |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| **File formats (YAML/JSON/TOML/INI/.env)** | All 5 | All 5 | 4 (no INI) | None | 4 (no INI) | 3 (no INI/.env) |
| **Environment variables** | Yes | Yes | Yes | Yes | Yes | Yes |
| **Cloud sources (S3, SSM, Azure, GCS)** | All 4 + IBM COS, Git | No (etcd/Consul only) | S3, Consul, etcd | No | No | Consul, etcd |
| **Secret stores (Vault, AWS, Azure, GCP)** | All 4 + env, multi | No | Vault only | No | No | Vault only |
| **Deep merge** | Yes | Partial | Yes | No | No | Unclear |
| **Per-path merge strategies** | Yes (6 strategies) | No | Global only | No | No | No |
| **Config composition (_include/_defaults)** | Yes | No | No | No | No | No |
| **Environment resolution (default+prod)** | Built-in | No | No | No | No | No |
| **Type-safe generics (`Config[T]`)** | Yes | No | No | No | No | No |
| **Struct tag validation** | Yes | No | No | Partial | Partial | No |
| **JSON Schema validation** | Yes | No | No | No | No | No |
| **File watching / hot reload** | Yes | Yes | Yes | No | No | Yes |
| **Incremental reload** | Yes | No | No | No | No | No |
| **Hook/middleware system** | Yes (4 types) | No | No | No | No | Partial |
| **`${secret:key}` placeholders** | Yes | No | No | No | No | No |
| **`${VAR}` expansion in values** | Yes | No | No | No | No | No |
| **Source tracking / introspection** | Yes | No | No | No | No | No |
| **Config diff / drift detection** | Yes | No | No | No | No | No |
| **Config versioning / rollback** | Yes | No | No | No | No | No |
| **Observability (metrics, events)** | Yes | No | No | No | No | Prometheus |
| **CLI tool** | Yes (10 commands) | No | No | No | No | No |
| **Doc generation** | Yes (markdown/JSON) | No | No | No | No | No |
| **Builder pattern** | Yes | No | No | No | No | No |
| **Thread-safe** | Yes (RWMutex) | No | Partial | N/A | No | Yes |
| **Self-configuration file** | Yes | No | No | No | No | No |

### What Confii solves that others don't

**1. The multi-source merge problem.** Viper's deep merge has [known limitations](https://github.com/spf13/viper/issues/181) with slices and nested maps. Confii provides 6 merge strategies (replace, merge, append, prepend, intersection, union) with per-path overrides — so `database` can use `replace` while `features` uses `append` in the same merge.

**2. Secret management as a first-class concern.** No other Go config library natively resolves `${secret:db/password}` placeholders from AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, or HashiCorp Vault (with 9 auth methods) — with caching, TTL, and a pluggable store interface. In other libraries, you write custom glue code for every project.

**3. Environment-aware configuration.** Confii natively understands `default` + `production`/`staging`/`development` sections and merges them automatically. Other libraries require you to manually load separate files per environment.

**4. Type safety with Go generics.** `Config[AppConfig]` gives you `cfg.Typed()` returning `*AppConfig` with struct tag validation — no other Go config library uses generics for this.

**5. Configuration lifecycle management.** Diff two configs, detect drift from a baseline, snapshot versions and rollback, track access metrics, emit events on change — features that matter in production but don't exist in any other Go config library.

**6. Full introspection.** `Explain("database.host")` tells you the value, where it came from, how many times it was overridden, and the full override history. `Layers()` shows the source stack. `GenerateDocs("markdown")` produces a reference table. No other Go config library offers this.

**7. Thread safety.** Viper has [documented concurrency issues](https://github.com/spf13/viper/issues/268). Confii uses `sync.RWMutex` throughout — concurrent reads are lock-free, writes are serialized.

## Installation

```bash
go get github.com/qualitycoe/confii-go
```

### Optional cloud dependencies (via build tags)

```bash
# AWS (S3, SSM, Secrets Manager)
go build -tags aws

# Azure (Blob Storage, Key Vault)
go build -tags azure

# GCP (Cloud Storage, Secret Manager)
go build -tags gcp

# HashiCorp Vault
go build -tags vault

# IBM Cloud Object Storage
go build -tags ibm

# All cloud providers
go build -tags "aws,azure,gcp,vault,ibm"
```

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    confii "github.com/qualitycoe/confii-go"
    "github.com/qualitycoe/confii-go/loader"
)

func main() {
    cfg, err := confii.New[any](context.Background(),
        confii.WithLoaders(
            loader.NewYAML("config/base.yaml"),
            loader.NewYAML("config/production.yaml"),
        ),
        confii.WithEnv("production"),
    )
    if err != nil {
        log.Fatal(err)
    }

    host, _ := cfg.Get("database.host")
    port := cfg.GetIntOr("database.port", 5432)
    debug := cfg.GetBoolOr("debug", false)

    fmt.Printf("Host: %s, Port: %d, Debug: %v\n", host, port, debug)
}
```

### Typed Access with Generics

```go
type AppConfig struct {
    Database DatabaseConfig `mapstructure:"database"`
    Debug    bool           `mapstructure:"debug"`
}

type DatabaseConfig struct {
    Host     string `mapstructure:"host" validate:"required"`
    Port     int    `mapstructure:"port" validate:"required,min=1,max=65535"`
    Name     string `mapstructure:"name" validate:"required"`
    Password string `mapstructure:"password"`
}

cfg, err := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
)
if err != nil {
    log.Fatal(err)
}

// Type-safe access — IDE autocomplete works here
model, err := cfg.Typed()
fmt.Println(model.Database.Host)  // "localhost"
fmt.Println(model.Database.Port)  // 5432
```

### Builder Pattern

```go
cfg, err := confii.NewBuilder[AppConfig]().
    WithEnv("production").
    AddLoader(loader.NewYAML("base.yaml")).
    AddLoader(loader.NewYAML("prod.yaml")).
    EnableFreezeOnLoad().
    Build(ctx)
```

## Configuration Sources

### File Loaders

```go
loader.NewYAML("config.yaml")       // YAML (.yaml, .yml)
loader.NewJSON("config.json")       // JSON
loader.NewTOML("config.toml")       // TOML
loader.NewINI("config.ini")         // INI (.ini, .cfg)
loader.NewEnvFile(".env")           // .env files
```

### Environment Variables

```go
// Loads APP_DATABASE__HOST → {"database": {"host": "..."}}
loader.NewEnvironment("APP")

// With custom separator
loader.NewEnvironment("APP", loader.WithSeparator("_"))
```

### HTTP

```go
loader.NewHTTP("https://config-server.example.com/app.json",
    loader.WithTimeout(10 * time.Second),
    loader.WithHeaders(map[string]string{"Authorization": "Bearer token"}),
    loader.WithBasicAuth("user", "pass"),
)
```

### Cloud Loaders (build-tag gated)

```go
// AWS S3 (requires -tags aws)
s3Loader, _ := cloud.NewS3("s3://my-bucket/config.yaml",
    cloud.WithS3Region("us-west-2"),
)

// AWS SSM Parameter Store (requires -tags aws)
ssmLoader := cloud.NewSSM("/myapp/production/")

// Azure Blob Storage (requires -tags azure)
azLoader := cloud.NewAzureBlob("my-container", "config.yaml")

// Google Cloud Storage (requires -tags gcp)
gcsLoader := cloud.NewGCS("my-bucket", "config.yaml")

// Git (GitHub/GitLab — no build tag needed)
gitLoader := cloud.NewGit(
    "https://github.com/org/config-repo", "app/config.yaml",
    cloud.WithGitBranch("main"),
    cloud.WithGitToken(os.Getenv("GIT_TOKEN")),
)
```

## Configuration Composition

Use `_include` and `_defaults` directives for Hydra-style config composition:

```yaml
# app.yaml
_defaults:
  - "timeout: 30"
  - cache: redis

_include:
  - shared/logging.yaml
  - shared/database.yaml

app:
  name: my-service
```

Included files are resolved relative to the source file's directory, processed recursively (with cycle detection and max depth of 10), and merged into the config. The `_include`, `_defaults`, and `_merge_strategy` keys are removed from the final output.

## Merging

Later loaders override earlier ones. Deep merge is enabled by default.

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("defaults.yaml"),  // base
        loader.NewYAML("overrides.yaml"), // overrides
    ),
    confii.WithDeepMerge(true),
)
```

### Advanced Merge Strategies

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(base, overlay),
    confii.WithMergeStrategyOption(confii.StrategyMerge),
    confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
        "database":     confii.StrategyReplace,  // replace entire section
        "app.features": confii.StrategyAppend,   // append feature lists
    }),
)
```

Available strategies: `Replace`, `Merge`, `Append`, `Prepend`, `Intersection`, `Union`.

## Environment Resolution

Config files with `default` + environment sections are automatically resolved:

```yaml
# config.yaml
default:
  database:
    host: localhost
    port: 5432
  debug: true

production:
  database:
    host: prod-db.example.com
  debug: false
```

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("production"),
)
// database.host = "prod-db.example.com" (from production)
// database.port = 5432                  (from default)
// debug = false                         (from production)
```

Use `WithEnvSwitcher("APP_ENV")` to read the environment name from an OS variable.

## Secret Management

```go
import "github.com/qualitycoe/confii-go/secret"

store := secret.NewDictStore(map[string]any{
    "db/password": "s3cret",
    "api/key":     "abc123",
})

resolver := secret.NewResolver(store,
    secret.WithCache(true),
    secret.WithCacheTTL(5 * time.Minute),
    secret.WithResolverPrefix("prod/"),
)

cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
)
cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())

// Now ${secret:db/password} in config values is resolved automatically
// Supports ${secret:key}, ${secret:key:json_path}, ${secret:key:json_path:version}
```

### Cloud Secret Stores (build-tag gated)

```go
// AWS Secrets Manager (requires -tags aws)
awsStore, _ := cloud.NewAWSSecretsManager(ctx,
    cloud.WithAWSRegion("us-east-1"),
)

// HashiCorp Vault (requires -tags vault) — 9 auth methods
vaultStore, _ := cloud.NewHashiCorpVault(
    cloud.WithVaultURL("https://vault.example.com"),
    cloud.WithVaultAuth(&cloud.AppRoleAuth{RoleID: "...", SecretID: "..."}),
    cloud.WithVaultMountPoint("secret"),
)

// Azure Key Vault (requires -tags azure)
azStore, _ := cloud.NewAzureKeyVault("https://my-vault.vault.azure.net", nil)

// GCP Secret Manager (requires -tags gcp)
gcpStore, _ := cloud.NewGCPSecretManager(ctx, "my-project-id")

// Multi-store fallback chain
multi := secret.NewMultiStore([]confii.SecretStore{awsStore, vaultStore})
```

### Vault Authentication Methods

Token, AppRole, LDAP, JWT, Kubernetes, AWS IAM, Azure, GCP, and OIDC.

## Validation

### Struct Tags (validate on load)

```go
type Config struct {
    Host string `mapstructure:"host" validate:"required,hostname"`
    Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
}

cfg, err := confii.New[Config](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
    confii.WithStrictValidation(true),
)
// Returns error at construction time if validation fails
```

### JSON Schema

```go
import "github.com/qualitycoe/confii-go/validate"

v, _ := validate.NewJSONSchemaValidatorFromFile("schema.json")
err := v.Validate(cfg.ToDict())
```

## Lifecycle

### Reload

```go
// Full reload
err := cfg.Reload(ctx)

// Incremental (only changed files, based on mtime + SHA256)
err := cfg.Reload(ctx, confii.WithIncremental(true))

// Dry run (load and validate without applying)
err := cfg.Reload(ctx, confii.WithDryRun(true))

// With validation override
err := cfg.Reload(ctx, confii.WithReloadValidate(true))
```

### Extend (add loader at runtime)

```go
err := cfg.Extend(ctx, loader.NewJSON("extra-config.json"))
// New keys are immediately available
```

### Freeze

```go
cfg.Freeze()
err := cfg.Set("key", "val")  // returns ErrConfigFrozen
```

### Override (temporary)

```go
restore, _ := cfg.Override(map[string]any{"database.host": "test-db"})
defer restore()
// config uses overridden values in this scope
```

### Set with guard

```go
// Normal set (overwrites)
cfg.Set("key", "value")

// Protected set (errors if key exists)
err := cfg.Set("key", "value", confii.WithOverride(false))
```

### Change Callbacks

```go
cfg.OnChange(func(key string, oldVal, newVal any) {
    log.Printf("Config changed: %s = %v → %v", key, oldVal, newVal)
})
```

### File Watching

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithDynamicReloading(true),
)
// Config auto-reloads when files change

cfg.StopWatching() // stop when done
```

## Introspection

### Explain a key

```go
info := cfg.Explain("database.host")
// {
//   "exists": true,
//   "value": "prod-db.example.com",
//   "source": "config.yaml",
//   "loader_type": "YAMLLoader",
//   "environment": "production",
//   "override_count": 1,
//   "override_history": [{"value": "localhost", "source": "defaults.yaml", ...}],
//   "current_value": "prod-db.example.com"
// }
```

### Source tracking

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(...),
    confii.WithDebugMode(true),
)

info := cfg.GetSourceInfo("database.host")       // SourceInfo struct
history := cfg.GetOverrideHistory("database.host") // []OverrideEntry
conflicts := cfg.GetConflicts()                    // all overridden keys
stats := cfg.GetSourceStatistics()                 // aggregated stats
keys := cfg.FindKeysFromSource("config.yaml")      // keys from specific source
```

### Layers

```go
layers := cfg.Layers()
// [
//   {"source": "base.yaml", "loader_type": "YAMLLoader", "key_count": 12, "keys": [...]},
//   {"source": "overrides.yaml", "loader_type": "YAMLLoader", "key_count": 4, "keys": [...]},
// ]
```

### Schema info

```go
info := cfg.Schema("database.port")
// {"key": "database.port", "exists": true, "type": "int", "value": 5432}
```

### Debug report

```go
fmt.Print(cfg.PrintDebugInfo("database.host")) // human-readable
cfg.ExportDebugReport("debug_report.json")     // full JSON report
```

### Documentation generation

```go
markdown, _ := cfg.GenerateDocs("markdown")
jsonDocs, _ := cfg.GenerateDocs("json")
```

## Observability

```go
// Enable on the Config instance
metrics := cfg.EnableObservability()
emitter := cfg.EnableEvents()

emitter.On("reload", func(args ...any) { log.Println("Config reloaded") })
emitter.On("change", func(args ...any) { log.Println("Config changed") })

stats := cfg.GetMetrics()  // access rate, reload count, top keys, etc.
```

## Versioning

```go
vm := cfg.EnableVersioning("/tmp/config-versions", 100)

v1, _ := cfg.SaveVersion(map[string]any{"author": "deploy-bot", "env": "production"})

// ... config changes ...
v2, _ := cfg.SaveVersion(nil)

// Rollback
cfg.RollbackToVersion(v1.VersionID)

// Compare versions
diffs, _ := vm.DiffVersions(v1.VersionID, v2.VersionID)
```

## Diff & Drift Detection

```go
// Compare two Config instances
diffs := cfg1.Diff(cfg2)

// Detect drift from intended baseline
drifts := cfg.DetectDrift(intendedConfig)

// Or use standalone
diffs := diff.Diff(config1, config2)
summary := diff.Summary(diffs)  // {total: 3, added: 1, removed: 1, modified: 1}
```

## Export

```go
// To bytes
jsonData, _ := cfg.Export("json")
yamlData, _ := cfg.Export("yaml")
tomlData, _ := cfg.Export("toml")

// To file
cfg.Export("json", "/path/to/output.json")
```

## Self-Configuration

Confii reads its own settings from a `.confii.yaml` (or `.json`/`.toml`) file automatically before any user loaders run. Settings are applied with 3-tier priority: **explicit argument > self-config > built-in default**.

```yaml
# .confii.yaml
default_environment: development
env_prefix: APP
deep_merge: true
use_env_expander: true
validate_on_load: false
default_files:
  - config/base.yaml
  - config/dev.yaml
```

Search order: CWD (`confii.*`, `.confii.*`), then `~/.config/confii/`.

## CLI

```bash
go install github.com/qualitycoe/confii-go-go/cmd/confii@latest
```

```bash
# Load and display config
confii load production -l yaml:config.yaml

# Get a single value
confii get production database.host -l yaml:config.yaml

# Export to different format
confii export production -l yaml:config.yaml -f json -o config.json

# Validate against JSON Schema
confii validate production -l yaml:config.yaml --schema schema.json

# Compare environments
confii diff development production \
  --loader1 yaml:config.yaml --loader2 yaml:config.yaml

# Debug source tracking
confii debug production -l yaml:config.yaml --key database.host

# Explain key resolution
confii explain production -l yaml:config.yaml --key database.host

# Lint for issues
confii lint production -l yaml:config.yaml --strict

# Generate documentation
confii docs production -l yaml:config.yaml -f markdown -o CONFIG_DOCS.md

# Migrate from other tools
confii migrate dotenv .env -o config.yaml
```

## Package Structure

```
github.com/qualitycoe/confii-go/
  ├── config.go            # Config[T] struct, New(), all access/lifecycle/introspection methods
  ├── builder.go           # Fluent builder API
  ├── errors.go            # Sentinel errors + ConfigError
  ├── loader/              # File & env loaders (YAML, JSON, TOML, INI, .env, HTTP)
  │   └── cloud/           # Cloud loaders (S3, SSM, Azure Blob, GCS, IBM COS, Git)
  ├── secret/              # Secret stores (env, dict, multi) + resolver
  │   └── cloud/           # Cloud secret stores (AWS, Azure, GCP, Vault + 9 auth methods)
  ├── merge/               # Merge strategies (default + advanced with per-path overrides)
  ├── compose/             # Configuration composition (_include, _defaults)
  ├── envhandler/          # Environment resolution (default + env sections)
  ├── hook/                # Hook processor (4 types: key, value, condition, global)
  ├── validate/            # Struct tag + JSON Schema validation
  ├── observe/             # Metrics, events, versioning with rollback
  ├── diff/                # Diff + drift detection
  ├── sourcetrack/         # Per-key source tracking, override history, file change detection
  ├── watch/               # File watching (fsnotify)
  ├── export/              # JSON, YAML, TOML exporters
  ├── selfconfig/          # Self-configuration reader (.confii.yaml)
  ├── internal/            # Internal utilities (dictutil, typecoerce, formatparse)
  ├── integration/         # End-to-end integration tests (50 tests, no mocks)
  └── cmd/confii/    # CLI tool (10 commands)
```

## Requirements

- Go 1.21+ (for `log/slog` and generics)

## License

[MIT](LICENSE)
