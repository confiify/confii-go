<!-- markdownlint-disable MD033 MD041 -->
<p align="center">
  <img src="art/confii-go.png" alt="Confii Logo" />
</p>

<p align="center">
  <strong>Complete configuration management for Go.</strong><br>
  Load, merge, validate, resolve secrets, track sources, detect drift — from any source.
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/confiify/confii-go"><img src="https://pkg.go.dev/badge/github.com/confiify/confii-go.svg" alt="Go Reference"></a>
  <a href="https://github.com/confiify/confii-go/actions/workflows/ci.yaml"><img src="https://github.com/confiify/confii-go/actions/workflows/ci.yaml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/confiify/confii-go"><img src="https://codecov.io/gh/confiify/confii-go/branch/main/graph/badge.svg" alt="Coverage"></a>
  <a href="https://goreportcard.com/report/github.com/confiify/confii-go"><img src="https://goreportcard.com/badge/github.com/confiify/confii-go" alt="Go Report Card"></a>
  <a href="https://securityscorecards.dev/viewer/?uri=github.com/confiify/confii-go"><img src="https://api.securityscorecards.dev/projects/github.com/confiify/confii-go/badge" alt="OpenSSF Scorecard"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>
<!-- markdownlint-enable MD033 MD041 -->

---

## Table of Contents

- [Why Confii?](#why-confii)
- [Installation](#installation)
- [Quick Start](#quick-start)
- **Configuring Confii**
  - [Creating a Config Instance](#creating-a-config-instance) — constructor, builder, self-config, options
  - [Configuration Sources](#configuration-sources) — files, env vars, HTTP, cloud
  - [Configuration Composition](#configuration-composition) — `_include`, `_defaults`
  - [Environment Resolution](#environment-resolution) — `default` + env-specific merging
  - [Merge Strategies](#merge-strategies) — 6 strategies with per-path overrides
- **Working with Values**
  - [Accessing Values](#accessing-values) — `Get`, typed getters, `Typed()`
  - [Hooks & Transformation](#hooks--transformation) — 4 hook types, `${VAR}` expansion
  - [Validation](#validation) — struct tags, JSON Schema
  - [Secret Management](#secret-management) — `${secret:key}`, cloud stores
- **Runtime & Operations**
  - [Lifecycle Management](#lifecycle-management) — reload, extend, freeze, override
  - [Dynamic Reloading](#dynamic-reloading) — file watching via fsnotify
  - [Observability](#observability) — metrics, events
- **Debugging & Auditing**
  - [Introspection & Source Tracking](#introspection--source-tracking) — Explain, Layers, debug reports
  - [Diff & Drift Detection](#diff--drift-detection) — compare configs, detect unintended changes
  - [Versioning & Rollback](#versioning--rollback) — snapshots, compare, restore
- **Output**
  - [Export](#export) — JSON, YAML, TOML
  - [Documentation Generation](#documentation-generation)
- [CLI Tool](#cli-tool)
- [Examples](#examples)
- [Package Structure](#package-structure)

---

## Why Confii?

Go has several configuration libraries, but none provides a complete configuration *management* solution. Most handle loading and reading — Confii handles the full lifecycle.

| Capability | Confii | Viper | Koanf | Others |
| --- | :---: | :---: | :---: | :---: |
| File formats (YAML/JSON/TOML/INI/.env) | All 5 | All 5 | 4 | Partial |
| Cloud sources (S3, SSM, Azure, GCS, IBM, Git) | All 6 | etcd/Consul | S3, etcd | Limited |
| Secret stores (Vault, AWS, Azure, GCP) | All 4 + env | No | Vault | Limited |
| Per-path merge strategies (6 strategies) | Yes | No | Global only | No |
| Config composition (`_include`/`_defaults`) | Yes | No | No | No |
| Type-safe generics (`Config[T]`) | Yes | No | No | No |
| `${secret:key}` placeholder resolution | Yes | No | No | No |
| Source tracking / introspection | Yes | No | No | No |
| Config diff / drift detection | Yes | No | No | No |
| Versioning with rollback | Yes | No | No | No |
| Observability (metrics, events) | Yes | No | No | No |
| Hook/middleware system (4 types) | Yes | No | No | No |
| File watching + incremental reload | Yes | Yes | Yes | Partial |
| JSON Schema validation | Yes | No | No | No |
| CLI tool (10 commands) | Yes | No | No | No |
| Thread-safe (RWMutex) | Yes | [No](https://github.com/spf13/viper/issues/268) | Partial | Varies |

<!-- markdownlint-disable MD033 -->
<details>
<summary><strong>What Confii solves that others don't</strong></summary>
<!-- markdownlint-enable MD033 -->

**1. The multi-source merge problem.** Viper's deep merge has [known limitations](https://github.com/spf13/viper/issues/181) with slices and nested maps. Confii provides 6 merge strategies with per-path overrides — so `database` can use `replace` while `features` uses `append` in the same merge.

**2. Secret management as a first-class concern.** No other Go config library natively resolves `${secret:db/password}` placeholders from AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, or HashiCorp Vault (with 9 auth methods) — with caching, TTL, and a pluggable store interface.

**3. Environment-aware configuration.** Confii natively understands `default` + `production`/`staging`/`development` sections and merges them automatically. Other libraries require you to manually load separate files per environment.

**4. Type safety with Go generics.** `Config[AppConfig]` gives you `cfg.Typed()` returning `*AppConfig` with struct tag validation — no other Go config library uses generics for this.

**5. Configuration lifecycle management.** Diff two configs, detect drift from a baseline, snapshot versions and rollback, track access metrics, emit events on change — features that matter in production but don't exist elsewhere.

**6. Full introspection.** `Explain("database.host")` tells you the value, where it came from, how many times it was overridden, and the full history. `Layers()` shows the source stack. `GenerateDocs("markdown")` produces a reference table.

</details>

---

## Installation

```bash
go get github.com/confiify/confii-go
```

Cloud providers are opt-in via build tags:

```bash
go build -tags aws          # S3, SSM, Secrets Manager
go build -tags azure        # Blob Storage, Key Vault
go build -tags gcp          # Cloud Storage, Secret Manager
go build -tags vault        # HashiCorp Vault
go build -tags ibm          # IBM Cloud Object Storage
go build -tags "aws,azure,gcp,vault,ibm"  # all
```

---

## Quick Start

```go
cfg, err := confii.New[any](context.Background(),
    confii.WithLoaders(
        loader.NewYAML("config.yaml"),
        loader.NewEnvironment("APP"),
    ),
    confii.WithEnv("production"),
)

host, _ := cfg.Get("database.host")
port := cfg.GetIntOr("database.port", 5432)
debug := cfg.GetBoolOr("debug", false)
```

> **Full example:** [`examples/basic/`](examples/basic/main.go)

---

## Configuring Confii

Before diving into features, it's important to understand the three ways to configure a Confii instance and what options are available. This determines how your config is loaded, merged, validated, and accessed.

### Creating a Config Instance

There are three ways to create a `Config[T]` instance, listed from simplest to most flexible:

**1. Self-configuration file** (zero-code defaults) — Confii auto-discovers a `.confii.yaml` (or `.json`/`.toml`) file and applies settings *before* any code runs. This is the best place for project-wide defaults that every developer shares.

```yaml
# .confii.yaml — auto-discovered from CWD or ~/.config/confii/
default_environment: development
env_prefix: APP
deep_merge: true
use_env_expander: true
validate_on_load: false
default_files:
  - config/base.yaml
  - config/dev.yaml
```

Settings apply with 3-tier priority: **explicit code argument > self-config file > built-in default**. Search order: CWD (`confii.*`, `.confii.*`), then `~/.config/confii/`.

> **Full example:** [`examples/self-config/`](examples/self-config/main.go)

**2. Constructor with options** — Pass `With*` option functions directly:

```go
cfg, err := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("production"),
    confii.WithDeepMerge(true),
    confii.WithValidateOnLoad(true),
)
```

**3. Builder pattern** — Fluent API with chained methods, useful when config construction is conditional or spans multiple steps:

```go
cfg, err := confii.NewBuilder[AppConfig]().
    WithEnv("production").
    AddLoader(loader.NewYAML("base.yaml")).
    AddLoader(loader.NewYAML("prod.yaml")).
    EnableDeepMerge().
    EnableFreezeOnLoad().
    Build(ctx)
```

> **Full example:** [`examples/builder/`](examples/builder/main.go)

**Available options:**

| Option | Purpose | Default |
| --- | --- | --- |
| `WithLoaders(loaders...)` | Set configuration sources | none |
| `WithEnv(name)` | Set active environment (e.g. `"production"`) | `""` |
| `WithEnvSwitcher(envVar)` | Read environment name from OS variable | none |
| `WithEnvPrefix(prefix)` | Auto-add an `EnvironmentLoader` with this prefix | none |
| `WithDeepMerge(bool)` | Enable recursive merge of nested maps | `true` |
| `WithMergeStrategyOption(strategy)` | Default merge strategy | `Merge` |
| `WithMergeStrategyMap(map)` | Per-path merge strategy overrides | none |
| `WithValidateOnLoad(bool)` | Validate struct tags after loading | `false` |
| `WithStrictValidation(bool)` | Treat validation warnings as errors | `false` |
| `WithSchema(schema)` / `WithSchemaPath(path)` | JSON Schema for validation | none |
| `WithEnvExpander(bool)` | Enable `${VAR}` expansion in values | `false` |
| `WithTypeCasting(bool)` | Auto-convert strings to bool/int/float | `false` |
| `WithSysenvFallback(bool)` | Fall back to OS env vars on missing keys | `false` |
| `WithDynamicReloading(bool)` | Enable fsnotify file watching | `false` |
| `WithFreezeOnLoad(bool)` | Make config immutable after load | `false` |
| `WithDebugMode(bool)` | Enable full source tracking | `false` |
| `WithOnError(policy)` | `ErrorPolicyRaise`, `Warn`, or `Ignore` | `Raise` |
| `WithLogger(logger)` | Custom `*slog.Logger` | default |

---

### Configuration Sources

Confii loads from files, environment variables, HTTP, and cloud storage — all through a unified `Loader` interface. Later loaders override earlier ones with deep merge enabled by default.

| Source | Constructor | Build Tag |
| --- | --- | --- |
| YAML | `loader.NewYAML(path)` | - |
| JSON | `loader.NewJSON(path)` | - |
| TOML | `loader.NewTOML(path)` | - |
| INI | `loader.NewINI(path)` | - |
| .env | `loader.NewEnvFile(path)` | - |
| Environment vars | `loader.NewEnvironment(prefix)` | - |
| HTTP/HTTPS | `loader.NewHTTP(url, opts...)` | - |
| AWS S3 | `cloud.NewS3(url, opts...)` | `aws` |
| AWS SSM | `cloud.NewSSM(prefix)` | `aws` |
| Azure Blob | `cloud.NewAzureBlob(container, blob)` | `azure` |
| GCS | `cloud.NewGCS(bucket, object)` | `gcp` |
| IBM COS | `cloud.NewIBMCOS(...)` | `ibm` |
| Git repo | `cloud.NewGit(repo, path, opts...)` | - |

> **Full example:** [`examples/multi-source/`](examples/multi-source/main.go) | [`examples/cloud/`](examples/cloud/main.go)

---

### Configuration Composition

Hydra-style `_include` and `_defaults` directives let you split config across files with cycle detection (max depth 10):

```yaml
_defaults:
  - "timeout: 30"
  - cache: redis

_include:
  - shared/logging.yaml
  - shared/database.yaml

app:
  name: my-service
```

Included files are resolved relative to the source file's directory. Directive keys (`_include`, `_defaults`, `_merge_strategy`) are removed from the final output.

> **Full example:** [`examples/composition/`](examples/composition/main.go)

---

### Environment Resolution

Config files with `default` + environment-specific sections are automatically merged:

```yaml
default:
  database:
    host: localhost
    port: 5432

production:
  database:
    host: prod-db.example.com
```

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("production"),         // explicit
    // confii.WithEnvSwitcher("APP_ENV"), // or from OS variable
)
// database.host = "prod-db.example.com" (production)
// database.port = 5432                  (inherited from default)
```

> **Full example:** [`examples/environment/`](examples/environment/main.go)

---

### Merge Strategies

Six strategies with per-path overrides — different sections can merge differently:

| Strategy | Behavior |
| --- | --- |
| `Replace` | Overwrites entirely |
| `Merge` | Recursive deep merge |
| `Append` | Appends list items |
| `Prepend` | Prepends list items |
| `Intersection` | Keeps only common keys |
| `Union` | Keeps all keys, merges common ones |

```go
confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
    "database": confii.StrategyReplace,
    "features": confii.StrategyAppend,
})
```

> **Full example:** [`examples/merge-strategies/`](examples/merge-strategies/main.go)

---

## Working with Values

Once a `Config[T]` instance is created and loaded, here's how you access, transform, validate, and resolve values.

### Accessing Values

**Untyped access** with dot-notation key paths:

```go
val, err := cfg.Get("database.host")           // (any, error)
val := cfg.GetOr("database.host", "localhost")  // with default
val := cfg.MustGet("database.host")             // panics on error (for tests)
```

**Typed getters** with defaults:

```go
cfg.GetString("app.name")                   // (string, error)
cfg.GetStringOr("app.name", "default")      // with fallback
cfg.GetInt("database.port")                  // (int, error)
cfg.GetIntOr("database.port", 5432)
cfg.GetBool("debug")                         // (bool, error)
cfg.GetBoolOr("debug", false)
cfg.GetFloat64("threshold")                  // (float64, error)
```

**Typed struct access** with Go generics:

```go
cfg, err := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
)

model, _ := cfg.Typed()              // *AppConfig — IDE autocomplete works
fmt.Println(model.Database.Host)
```

**Other access methods:**

```go
cfg.Has("database.host")    // bool — key existence
cfg.Keys()                  // []string — all leaf keys
cfg.Keys("database")        // []string — keys under prefix
cfg.ToDict()                // map[string]any — raw map
cfg.Set("key", "value")     // set a value
```

> **Full example:** [`examples/basic/`](examples/basic/main.go) | [`examples/typed/`](examples/typed/main.go)

---

### Hooks & Transformation

Hooks transform values at access time. Four types, evaluated in order: key → value → condition → global.

| Hook Type | Fires When |
| --- | --- |
| **Key hook** | Key exactly matches a registered path |
| **Value hook** | Value exactly matches a registered value |
| **Condition hook** | Custom condition function returns `true` |
| **Global hook** | Every value access |

```go
hp := cfg.HookProcessor()

// Key hook: uppercase a specific key's value
hp.RegisterKeyHook("app.name", func(key string, value any) any {
    return strings.ToUpper(value.(string))
})

// Global hook: mask passwords
hp.RegisterGlobalHook(func(key string, value any) any {
    if strings.Contains(key, "password") { return "****" }
    return value
})
```

**Built-in hooks** (enabled via options):

- `WithEnvExpander(true)` — replaces `${VAR}` with OS environment variables
- `WithTypeCasting(true)` — converts strings to bool/int/float automatically

> **Full example:** [`examples/hooks/`](examples/hooks/main.go)

---

### Validation

**Struct tags** — validate on load using `go-playground/validator`:

```go
type Config struct {
    Host string `mapstructure:"host" validate:"required,hostname"`
    Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
}

cfg, err := confii.New[Config](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
    confii.WithStrictValidation(true),  // treat warnings as errors
)
// Returns error at construction time if validation fails
```

**JSON Schema** — validate against a schema file:

```go
v, _ := validate.NewJSONSchemaValidatorFromFile("schema.json")
err := v.Validate(cfg.ToDict())
```

> **Full example:** [`examples/validation/`](examples/validation/main.go)

---

### Secret Management

Secrets are resolved via the hook system — register a secret resolver as a global hook, and `${secret:key}` placeholders in config values are automatically replaced.

```go
store := secret.NewDictStore(map[string]any{"db/password": "s3cret"})
resolver := secret.NewResolver(store,
    secret.WithCache(true),
    secret.WithCacheTTL(5 * time.Minute),
)

cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())
// ${secret:db/password} in values → "s3cret"
```

**Placeholder formats:** `${secret:key}`, `${secret:key:json_path}`, `${secret:key:json_path:version}`

**Cloud secret stores** (build-tag gated):

| Store | Constructor | Build Tag |
| --- | --- | --- |
| AWS Secrets Manager | `cloud.NewAWSSecretsManager(ctx, opts...)` | `aws` |
| HashiCorp Vault | `cloud.NewHashiCorpVault(opts...)` | `vault` |
| Azure Key Vault | `cloud.NewAzureKeyVault(url, cred)` | `azure` |
| GCP Secret Manager | `cloud.NewGCPSecretManager(ctx, project)` | `gcp` |

Vault supports 9 auth methods: Token, AppRole, LDAP, JWT, Kubernetes, AWS IAM, Azure, GCP, and OIDC.

**Multi-store fallback** — try stores in priority order:

```go
multi := secret.NewMultiStore([]confii.SecretStore{awsStore, vaultStore, envStore})
```

> **Full example:** [`examples/secrets/`](examples/secrets/main.go) | [`examples/cloud/`](examples/cloud/main.go)

---

## Runtime & Operations

Once your config is loaded and values are flowing, these features help you manage it in a running application.

### Lifecycle Management

```go
// Reload from sources
cfg.Reload(ctx)
cfg.Reload(ctx, confii.WithIncremental(true))  // only changed files (mtime + SHA256)
cfg.Reload(ctx, confii.WithDryRun(true))        // validate without applying
cfg.Reload(ctx, confii.WithReloadValidate(true)) // override validate-on-load

// Extend at runtime — add a new source without reloading everything
cfg.Extend(ctx, loader.NewJSON("extra.json"))

// Temporary override (scoped) — useful for tests
restore, _ := cfg.Override(map[string]any{"database.host": "test-db"})
defer restore()

// Set values (respects frozen state)
cfg.Set("key", "value")
cfg.Set("key", "value", confii.WithOverride(false))  // errors if key exists

// Freeze — make config immutable
cfg.Freeze()
cfg.Set("key", "val")  // returns ErrConfigFrozen

// Change callbacks — react to value changes
cfg.OnChange(func(key string, old, new any) {
    log.Printf("changed: %s = %v → %v", key, old, new)
})
```

> **Full example:** [`examples/lifecycle/`](examples/lifecycle/main.go)

---

### Dynamic Reloading

File watching via fsnotify. Config automatically reloads when source files change on disk:

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithDynamicReloading(true),
)
// Config auto-reloads when files change

cfg.OnChange(func(key string, old, new any) { /* react */ })
cfg.StopWatching() // stop when done
```

> **Full example:** [`examples/dynamic-reload/`](examples/dynamic-reload/main.go)

---

### Observability

Track access patterns, react to events:

```go
cfg.EnableObservability()
emitter := cfg.EnableEvents()

emitter.On("reload", func(args ...any) { log.Println("reloaded") })
emitter.On("change", func(args ...any) { log.Println("changed") })

stats := cfg.GetMetrics()
// total_keys, accessed_keys, access_rate, reload_count, change_count, top_accessed_keys
```

> **Full example:** [`examples/observability/`](examples/observability/main.go)

---

## Debugging & Auditing

For troubleshooting config issues, auditing changes, and understanding where values came from.

### Introspection & Source Tracking

Know exactly where every value came from and how it got there:

```go
cfg.Explain("database.host")              // value, source, override count, full history
cfg.Schema("database.port")               // type info for a key
cfg.Layers()                              // source stack (which files, in what order)

cfg.GetSourceInfo("database.host")        // SourceInfo struct
cfg.GetOverrideHistory("database.host")   // []OverrideEntry — full override chain
cfg.GetConflicts()                        // all keys that were overridden
cfg.GetSourceStatistics()                 // aggregated stats per source
cfg.FindKeysFromSource("config.yaml")     // which keys came from this file

cfg.PrintDebugInfo("database.host")       // human-readable report
cfg.ExportDebugReport("report.json")      // full JSON export
```

Enable `WithDebugMode(true)` for complete override history tracking.

> **Full example:** [`examples/introspection/`](examples/introspection/main.go)

---

### Diff & Drift Detection

Compare two configs or detect unintended changes against an intended baseline:

```go
diffs := cfg1.Diff(cfg2)                   // compare two Config instances
drifts := cfg.DetectDrift(intendedConfig)   // detect unintended changes
summary := diff.Summary(diffs)              // {total: 3, added: 1, removed: 1, modified: 1}
jsonStr, _ := diff.ToJSON(diffs)            // serialize for reporting
```

> **Full example:** [`examples/diff/`](examples/diff/main.go)

---

### Versioning & Rollback

Snapshot config state, compare versions over time, and rollback:

```go
vm := cfg.EnableVersioning("/tmp/config-versions", 100)

v1, _ := cfg.SaveVersion(map[string]any{"author": "deploy-bot", "env": "prod"})
// ... config changes ...
v2, _ := cfg.SaveVersion(nil)

diffs, _ := vm.DiffVersions(v1.VersionID, v2.VersionID)
versions := vm.ListVersions()   // all snapshots, newest first
cfg.RollbackToVersion(v1.VersionID)
```

> **Full example:** [`examples/versioning/`](examples/versioning/main.go)

---

## Output

### Export

```go
jsonData, _ := cfg.Export("json")
yamlData, _ := cfg.Export("yaml")
tomlData, _ := cfg.Export("toml")
cfg.Export("json", "/path/to/output.json")  // to file
```

> **Full example:** [`examples/export/`](examples/export/main.go)

### Documentation Generation

Generate a reference document from the current config:

```go
markdown, _ := cfg.GenerateDocs("markdown")
jsonDocs, _ := cfg.GenerateDocs("json")
```

---

## CLI Tool

```bash
go install github.com/confiify/confii-go/cmd/confii@latest
```

| Command | Description |
| --- | --- |
| `confii load` | Load and display configuration |
| `confii get` | Retrieve a single value |
| `confii export` | Export to a different format |
| `confii validate` | Validate against JSON Schema |
| `confii diff` | Compare two configs or environments |
| `confii debug` | Debug source tracking for a key |
| `confii explain` | Detailed resolution info for a key |
| `confii lint` | Lint config for issues |
| `confii docs` | Generate documentation |
| `confii migrate` | Migrate from other config formats |

```bash
confii load production -l yaml:config.yaml
confii get production database.host -l yaml:config.yaml
confii export production -l yaml:config.yaml -f json -o config.json
confii validate production -l yaml:config.yaml --schema schema.json
confii diff dev production --loader1 yaml:c.yaml --loader2 yaml:c.yaml
confii explain production -l yaml:config.yaml --key database.host
confii lint production -l yaml:config.yaml --strict
confii docs production -l yaml:config.yaml -f markdown -o DOCS.md
confii migrate dotenv .env -o config.yaml
```

---

## Examples

All examples are runnable and located in the [`examples/`](examples/) directory:

#### Getting Started

| Example | Description |
| --- | --- |
| [`basic`](examples/basic/main.go) | Load a YAML file, access values with dot notation |
| [`typed`](examples/typed/main.go) | Type-safe `Config[T]` with struct validation |
| [`builder`](examples/builder/main.go) | Fluent builder pattern |
| [`self-config`](examples/self-config/main.go) | `.confii.yaml` auto-discovery |

#### Loading & Merging

| Example | Description |
| --- | --- |
| [`multi-source`](examples/multi-source/main.go) | Multiple loaders + environment variables |
| [`environment`](examples/environment/main.go) | Environment-aware config (default + production) |
| [`merge-strategies`](examples/merge-strategies/main.go) | Per-path merge strategies |
| [`composition`](examples/composition/main.go) | `_include` and `_defaults` directives |
| [`cloud`](examples/cloud/main.go) | Cloud loaders and secret stores |

#### Processing & Validation

| Example | Description |
| --- | --- |
| [`hooks`](examples/hooks/main.go) | Key, value, condition, and global hooks |
| [`validation`](examples/validation/main.go) | Struct tags + JSON Schema validation |
| [`secrets`](examples/secrets/main.go) | Secret resolution with `${secret:key}` |

#### Runtime & Debugging

| Example | Description |
| --- | --- |
| [`lifecycle`](examples/lifecycle/main.go) | Reload, freeze, override, change callbacks |
| [`dynamic-reload`](examples/dynamic-reload/main.go) | File watching with fsnotify |
| [`introspection`](examples/introspection/main.go) | Explain, Layers, source tracking, debug |
| [`observability`](examples/observability/main.go) | Metrics and event emission |
| [`versioning`](examples/versioning/main.go) | Snapshot, compare, and rollback |
| [`diff`](examples/diff/main.go) | Diff configs and detect drift |
| [`export`](examples/export/main.go) | Export to JSON/YAML/TOML + doc generation |

```bash
cd examples/basic && go run .
```

---

## Package Structure

```text
github.com/confiify/confii-go/
  ├── config.go              # Config[T] — core type with all access/lifecycle methods
  ├── builder.go             # Fluent builder API
  ├── errors.go              # Sentinel errors + ConfigError
  ├── loader/                # File & env loaders (YAML, JSON, TOML, INI, .env, HTTP)
  │   └── cloud/             # Cloud loaders (S3, SSM, Azure Blob, GCS, IBM COS, Git)
  ├── secret/                # Secret stores (env, dict, multi) + resolver
  │   └── cloud/             # Cloud secret stores (AWS, Azure, GCP, Vault)
  ├── merge/                 # Merge strategies (default + advanced with per-path overrides)
  ├── compose/               # Configuration composition (_include, _defaults)
  ├── envhandler/            # Environment resolution (default + env sections)
  ├── hook/                  # Hook processor (4 types: key, value, condition, global)
  ├── validate/              # Struct tag + JSON Schema validation
  ├── observe/               # Metrics, events, versioning with rollback
  ├── diff/                  # Diff + drift detection
  ├── sourcetrack/           # Per-key source tracking, override history
  ├── watch/                 # File watching (fsnotify)
  ├── export/                # JSON, YAML, TOML exporters
  ├── selfconfig/            # Self-configuration reader (.confii.yaml)
  ├── internal/              # Internal utilities (dictutil, typecoerce, formatparse)
  ├── integration/           # End-to-end integration tests
  ├── examples/              # Runnable examples
  └── cmd/confii/            # CLI tool (10 commands)
```

## Requirements

- Go 1.25+ (due to cloud provider dependencies; core library uses Go 1.21 features)

## License

[MIT](LICENSE)
