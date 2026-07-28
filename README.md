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
  <a href="https://github.com/confiify/confii-go/releases/latest"><img src="https://img.shields.io/github/v/release/confiify/confii-go?sort=semver" alt="Latest Release"></a>
  <a href="https://github.com/confiify/confii-go/actions/workflows/ci.yaml"><img src="https://github.com/confiify/confii-go/actions/workflows/ci.yaml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/confiify/confii-go"><img src="https://codecov.io/gh/confiify/confii-go/branch/main/graph/badge.svg" alt="Coverage"></a>
  <a href="https://www.bestpractices.dev/projects/12279"><img src="https://www.bestpractices.dev/projects/12279/badge" alt="OpenSSF Best Practices"></a>
  <a href="https://www.bestpractices.dev/projects/12279"><img src="https://www.bestpractices.dev/projects/12279/baseline" alt="OpenSSF Baseline Level 3"></a>
  <a href="https://securityscorecards.dev/viewer/?uri=github.com/confiify/confii-go"><img src="https://api.securityscorecards.dev/projects/github.com/confiify/confii-go/badge" alt="OpenSSF Scorecard"></a>
  <a href="https://api.reuse.software/info/github.com/confiify/confii-go"><img src="https://api.reuse.software/badge/github.com/confiify/confii-go" alt="REUSE status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

<p align="center">
  <a href="SECURITY.md">Security Policy</a> ·
  <a href="security-insights.yml">Security Insights 2.2</a> ·
  <a href="docs/SECURITY_TOOLING.md">Security Tooling</a> ·
  <a href="docs/RELEASING.md">SBOMs &amp; Provenance</a>
</p>
<!-- markdownlint-enable MD033 MD041 -->

---

## Table of Contents

- [Why Confii?](#why-confii)
- [Security & Supply-Chain Assurance](#security--supply-chain-assurance)
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
| CLI tool (14 commands) | Yes | No | No | No |
| Thread-safe (RWMutex) | Yes | [No](https://github.com/spf13/viper/issues/268) | Partial | Varies |

<!-- markdownlint-disable MD033 -->
<details>
<summary><strong>What Confii solves that others don't</strong></summary>
<!-- markdownlint-enable MD033 -->

**1. The multi-source merge problem.** Viper's deep merge has [known limitations](https://github.com/spf13/viper/issues/181) with slices and nested maps. Confii provides 6 merge strategies with per-path overrides — so `database` can use `replace` while `features` uses `append` in the same merge.

**2. Secret management as a first-class concern.** Confii natively resolves `${secret:db/password}` placeholders from AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, HashiCorp Vault, and OpenBao — with caching, TTL, and a pluggable store interface. The Vault-compatible integration implements nine authentication flows; CI live-tests Token and AppRole against OpenBao, while the remaining flows have protocol-level tests and require provider-side identity configuration.

**3. Environment-aware configuration.** Confii supports both recommended named files (`config/default.yaml` + `config/{environment}.yaml`) and a single file with `default` + environment sections. Teams choose one primary model; explicit hybrid mode exists for controlled migrations.

**4. Type safety with Go generics.** `Config[AppConfig]` gives you `cfg.Typed()` returning `*AppConfig` with struct tag validation and full IDE autocomplete.

**5. Configuration lifecycle management.** Diff two configs, detect drift from a baseline, snapshot versions and rollback, track access metrics, emit events on change — features that matter in production but don't exist elsewhere.

**6. Full introspection.** `Explain("database.host")` tells you the value, where it came from, how many times it was overridden, and the full history. `Layers()` shows the source stack. `GenerateDocs("markdown")` produces a reference table.

</details>

---

## Security & Supply-Chain Assurance

Confii treats security claims as testable release controls. The repository
publishes machine-readable security metadata and continuously exercises the
security-sensitive integrations it advertises.

| Control | Evidence |
| --- | --- |
| Static and dependency analysis | CodeQL, Govulncheck, OSV-Scanner, dependency review, and Gitleaks run in CI |
| Fuzzing | All 11 native Go fuzz targets run in CI; Fuzz Introspector produces a call-graph and complexity report |
| OpenBao interoperability | CI tests Token and AppRole authentication plus KV v2 operations against digest-pinned OpenBao 2.6.1 |
| Security metadata | [`security-insights.yml`](security-insights.yml) conforms to OpenSSF Security Insights 2.2 |
| Release integrity | The release pipeline produces SPDX 2.3 SBOMs, OpenVEX records, checksums, Sigstore bundles, and SLSA provenance |
| SBOM interoperability | Every release SBOM is imported and re-exported through commit-pinned bomctl/protobom |
| Continuous policy monitoring | An audit-only Minder profile is published; hosted activation and its current status are documented separately |

See [Security Tooling](docs/SECURITY_TOOLING.md), the
[Security Policy](SECURITY.md), and the [Release Process](docs/RELEASING.md)
for boundaries and verification details.

---

## Installation

The library and CLI have different scopes. Run `go get` from the root of an
existing Go module; it updates that project's `go.mod` and `go.sum`. Run
`go install` from any directory; it installs the standalone CLI without
changing the current project.

```bash
# From the root of an existing Go module
go get github.com/confiify/confii-go@latest

# From any directory
go install github.com/confiify/confii-go/confii@latest
confii --version
```

Starting from an empty directory? Follow the complete sequence below; creating
the module before `go get` is required.

Cloud providers are opt-in through separate modules and build tags, so the
core install stays small. Add the cloud module you use; it owns compatible
provider SDK versions:

```bash
# Example: AWS
go get github.com/confiify/confii-go/loader/cloud@latest
go get github.com/confiify/confii-go/secret/cloud@latest
go build -tags aws ./...

# Other providers
go build -tags azure ./...
go build -tags gcp ./...
go build -tags vault ./...
go build -tags ibm ./...
```

See [docs/installation.md](docs/installation.md) for module and provider
details.

---

## Quick Start

From an empty directory, initialize a Go module and let Confii create the
recommended separate-file environment layout:

```bash
mkdir my-service
cd my-service
go mod init example.com/my-service
go get github.com/confiify/confii-go@latest
go install github.com/confiify/confii-go/confii@latest
confii --version
confii init
```

Choose **Separate files**. Confii generates a complete, documented
`.confii.yaml`, shared defaults in `config/default.yaml`, and development and
production override files. Load them without hard-coded paths:

```go
package main

import (
    "context"
    "fmt"
    "log"

    confii "github.com/confiify/confii-go"
)

type AppConfig struct {
    App struct {
        Name string `mapstructure:"name"`
    } `mapstructure:"app"`
    Server struct {
        Host string `mapstructure:"host"`
        Port int    `mapstructure:"port"`
    } `mapstructure:"server"`
    Log struct {
        Level string `mapstructure:"level"`
    } `mapstructure:"log"`
}

func main() {
    cfg, err := confii.New[AppConfig](context.Background())
    if err != nil {
        log.Fatal(err)
    }
    values, err := cfg.Typed()
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("%s listening on %s:%d (%s)\n",
        values.App.Name, values.Server.Host, values.Server.Port, values.Log.Level)
}
```

```bash
# Preview the exact layers before starting the application
confii env
confii env list
confii plan
APP_ENV=production confii plan

# Run with the generated development default, then production
go run .
APP_ENV=production go run .
```

See the [from-scratch Quick Start](docs/quickstart.md) for the generated files,
runtime overrides, the single-file alternative, and expected output.

---

## Configuring Confii

Before diving into features, it's important to understand the three ways to configure a Confii instance and what options are available. This determines how your config is loaded, merged, validated, and accessed.

### Creating a Config Instance

There are three ways to create a `Config[T]` instance, listed from simplest to most flexible:

**1. Self-configuration file** (zero-code defaults) — Confii auto-discovers a `.confii.yaml` (or `.json`/`.toml`) file and applies settings *before* any code runs. This is the best place for project-wide defaults that every developer shares.

Bootstrap a project interactively. Confii asks whether to use separate
environment files or one sectioned file, then creates the complete,
commented self-configuration and a loadable starter layout:

```bash
confii init
```

For automation, make the decision explicit:

```bash
# Recommended: config/default.yaml + config/{environment}.yaml
confii init --non-interactive --strategy named-files

# Alternative: config/application.yaml with environment sections
confii init --non-interactive --strategy sectioned
```

Initialization is idempotent. Confii detects every supported self-config
filename, reports an already initialized project without changing it, rejects
ambiguous initialization, rejects malformed existing configuration unless
`--force` is deliberately recovering the canonical `.confii.yaml`, and
preflights every planned output before writing. Use `--dry-run` to inspect the
plan and `--force` only for an
intentional replacement. `--force` replaces only the selected plan and never
deletes files from an older layout, so review obsolete files manually.
Application source is not generated or edited; the
runtime integration remains `confii.New[YourConfig](ctx)`.

```yaml
# .confii.yaml — auto-discovered from CWD or ~/.config/confii/
default_environment: development
env_switcher: APP_ENV
env_prefix: APP
environment_strategy: named_files
deep_merge: true
sources:
  - type: environment_files
    search_paths: [config]
    default_file: default.yaml
    environment_file: "{environment}.yaml"
```

For projects that keep one file per environment, opt in with an
`environment_files` source instead of hard-coding the selected file:

```yaml
# .confii.yaml
default_environment: development
env_switcher: APP_ENV
environment_strategy: named_files
sources:
  - type: environment_files
    search_paths: [config, .]
    default_file: default.yaml
    environment_file: "{environment}.yaml"
```

Confii loads the first `default.yaml` it finds, then the first file matching
the selected environment (for example `config/production.yaml`). The
environment file overrides the defaults. When `environment_strategy` is
omitted, declaring `environment_files` infers `named_files`. A normal flat
`type: yaml` source may still provide shared base values, but a file containing
top-level environment sections is rejected to prevent accidental mixed-model
precedence. Deliberate migrations can select `hybrid` with an explicit
`environment_conflict_policy` of `error`, `warn`, or `last_wins`.

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
| `WithEnvironmentStrategy(strategy)` | Select `Auto`, `Sectioned`, `NamedFiles`, or explicit `Hybrid` behavior | `Auto` |
| `WithEnvironmentConflictPolicy(policy)` | Control cross-model conflicts in hybrid mode | `LastWins` |
| `WithEnvPrefix(prefix)` | Auto-add an `EnvironmentLoader` with this prefix | none |
| `WithDeepMerge(bool)` | Enable recursive merge of nested maps | `true` |
| `WithMergeStrategyOption(strategy)` | Default merge strategy | `Merge` |
| `WithMergeStrategyMap(map)` | Per-path merge strategy overrides | none |
| `WithValidateOnLoad(bool)` | Validate struct tags after loading | `false` |
| `WithStrictValidation(bool)` | Treat validation warnings as errors | `false` |
| `WithSchema(schema)` / `WithSchemaPath(path)` | JSON Schema for validation | none |
| `WithEnvExpander(bool)` | Enable `${VAR}` expansion in values | `true` |
| `WithTypeCasting(bool)` | Auto-convert strings to bool/int/float | `true` |
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
| OpenBao | `cloud.NewOpenBao(opts...)` | `vault` |
| Azure Key Vault | `cloud.NewAzureKeyVault(url, cred)` | `azure` |
| GCP Secret Manager | `cloud.NewGCPSecretManager(ctx, project)` | `gcp` |

The Vault-compatible integration provides Token, AppRole, LDAP, JWT,
Kubernetes, AWS IAM, Azure, GCP, and OIDC authentication implementations.
Confii continuously tests token and AppRole KV operations against a real,
digest-pinned OpenBao server; the other methods have protocol-level fixtures
and require their corresponding server-side identity providers.

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
cfg.Reload(ctx, confii.WithIncremental(true))  // changed files only; remote sources refresh
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
go install github.com/confiify/confii-go/confii@latest
```

| Command | Description |
| --- | --- |
| `confii init` | Safely scaffold `.confii.yaml` and the selected environment layout |
| `confii env` | Show the effective environment; list or safely change the configured default |
| `confii connections test` | Perform value-safe reads against configured sources and secret providers |
| `confii load` | Load and display configuration |
| `confii get` | Retrieve a single value |
| `confii export` | Export to a different format |
| `confii validate` | Validate against JSON Schema |
| `confii diff` | Compare two configs or environments |
| `confii debug` | Debug source tracking for a key |
| `confii explain` | Detailed resolution info for a key |
| `confii plan` | Show environment strategy, source order, and mixed-model conflicts |
| `confii lint` | Lint config for issues |
| `confii docs` | Generate documentation |
| `confii migrate` | Migrate from other config formats |

```bash
confii init
confii env list
confii env set production
confii plan
confii connections test
APP_ENV=production confii load
confii get database.host

# Explicit loaders remain available for ad hoc files and automation
confii load production -l yaml:config.yaml
confii get production database.host -l yaml:config.yaml
confii export production -l yaml:config.yaml -f json -o config.json
confii validate production -l yaml:config.yaml --schema schema.json
confii diff dev production --loader1 yaml:c.yaml --loader2 yaml:c.yaml
confii explain production -l yaml:config.yaml --key database.host
confii plan production
confii lint production -l yaml:config.yaml --strict
confii docs production -l yaml:config.yaml -f markdown -o DOCS.md
confii migrate dotenv .env -o config.yaml
```

---

## Examples

The [`examples/`](examples/) directory contains focused, runnable feature
samples. For a realistic CRUD application with environment, LocalStack cloud,
Vault/OpenBao, and CLI walkthroughs, use the companion
[`confiify/confii-go-examples`](https://github.com/confiify/confii-go-examples)
repository.

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
  ├── config.go              # Config[T] core state and construction
  ├── config_*.go            # Access, mutation, reload, override, hooks, validation, etc.
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
  └── confii/                # CLI tool (14 commands)
```

## Requirements

- Go 1.25+ (cloud integrations are separate opt-in modules)

---

## Testing Philosophy

Confii ships with a high test bar because configuration libraries are
hard to debug in production: a silently-mishandled value or a
torn-state race surfaces as a failure in some downstream service hours
or days later. The audit cycles documented in
[REMEDIATION_PROGRESS.md](REMEDIATION_PROGRESS.md) and
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) raised the bar further.
Every contributor is expected to follow it.

### Three rules for new features

1. **Every new feature ships with negative tests.** A "negative test"
   asserts what the code refuses to do, not just what it accepts. If
   you add a new override, snapshot, copy, or comparison path, write
   a test that proves the path *cannot be misused*: a leaked pointer
   does not bleed into Config state, an out-of-order restore does not
   resurrect a popped frame, a type-drifted value still fires the
   change callback. Positive tests prove the happy path; negative
   tests prove the contract.

2. **Concurrency-touching code must pass under `-race`.** Any change
   that adds a goroutine, a `sync.*` primitive, a lock acquisition,
   or a snapshot-then-release pattern must include a test that
   exercises the new path under
   `go test ./<package>/ -race`. The audit found a CVE-class race
   (V-10) precisely because the affected `Export*` routine had no
   `-race` test. CI runs the full suite under `-race` on every PR.

3. **If you change a public contract documented in godoc, write the
   test that pins the documented behavior.** The audit found four
   contracts that the godoc promised but the implementation
   violated (V-03 deep-copy, V-04 GetSourceInfo isolation, V-05
   diff fidelity, V-09 panic logging). Each is now pinned by a
   regression test. New contracts get the same treatment from day
   one.

### What "negative" looks like in practice

```go
// V-03_a — []byte aliasing.
//
// Pre-V-23: DeepCopyValue([]byte("hello")) returned the SAME slice
// header. Mutating b[0] = 'X' bled through.
func TestDeepCopyValue_ByteSlice_NotAliased(t *testing.T) {
    src := []byte("hello")
    cp := DeepCopyValue(src).([]byte)
    src[0] = 'X'
    if string(cp) != "hello" {
        t.Fatalf("V-03 byte-slice aliasing: cp = %q, want %q", string(cp), "hello")
    }
}
```

The test deliberately mutates the source after the operation under
test and asserts the operation's output is unchanged. Run it against
the pre-fix code and it fails; run it against the post-fix code and
it passes. That asymmetry is the whole value of a negative test.

### Audit-pin tests live alongside the code

Negative tests for audit-closed vulnerabilities are conventionally
named `TestVNN_<scenario>` and live in `*_v23_test.go` or
`*_v24_test.go` files next to the implementation. Do not move them.
They are not "extra coverage" that can be deleted in a future
refactor — they are the load-bearing pins that prevent re-regression
of paid-for bugs.

### Running the suite

```bash
# Full suite, no race detector (fastest feedback):
go test ./...

# Full suite under -race (matches CI; required before opening a PR):
go test ./... -race

# Just the audit pins (Wave 23 + Wave 24):
go test ./... -run 'TestV0[1-9]_|TestV10_|TestDeepCopyValue_|TestNormalizeKeys_'

# Run all 11 native fuzz targets:
make fuzz FUZZTIME=30s

# Exercise every seed corpus without starting the fuzz engine:
make fuzz-seeds
```

A PR that adds new behavior without negative tests, or that fails
under `-race`, will not be merged.

---

## License

[MIT](LICENSE)
