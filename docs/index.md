---
hide:
  - navigation
  - toc
---

<p align="center">
  <img src="https://raw.githubusercontent.com/confiify/confii-go/main/art/confii-go.png" alt="Confii" width="400">
</p>

<p align="center">
  <strong>Complete Configuration Management for Go</strong>
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/confiify/confii-go"><img src="https://pkg.go.dev/badge/github.com/confiify/confii-go.svg" alt="Go Reference"></a>
  <a href="https://github.com/confiify/confii-go/releases/latest"><img src="https://img.shields.io/github/v/release/confiify/confii-go?sort=semver" alt="Latest Release"></a>
  <a href="https://github.com/confiify/confii-go/actions/workflows/ci.yaml"><img src="https://github.com/confiify/confii-go/actions/workflows/ci.yaml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/confiify/confii-go"><img src="https://codecov.io/gh/confiify/confii-go/branch/main/graph/badge.svg" alt="Coverage"></a>
  <a href="https://www.bestpractices.dev/projects/12279"><img src="https://www.bestpractices.dev/projects/12279/badge" alt="OpenSSF Best Practices"></a>
  <a href="https://www.bestpractices.dev/projects/12279"><img src="https://www.bestpractices.dev/projects/12279/baseline" alt="OpenSSF Baseline Level 3"></a>
  <a href="https://securityscorecards.dev/viewer/?uri=github.com/confiify/confii-go"><img src="https://api.securityscorecards.dev/projects/github.com/confiify/confii-go/badge" alt="OpenSSF Scorecard"></a>
  <a href="https://github.com/confiify/confii-go/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

---

Confii loads, merges, validates, and manages configuration from **any source** — YAML, JSON, TOML, INI, .env files, environment variables, HTTP endpoints, and cloud stores — with type-safe generics, secret resolution, source tracking, drift detection, and versioning.

For an application-sized walkthrough, see the
[`confiify/confii-go-examples`](https://github.com/confiify/confii-go-examples)
CRUD, LocalStack, and Vault/OpenBao companion repository.

## Features

- **Multi-source loading** — YAML, JSON, TOML, INI, .env, env vars, HTTP, S3, SSM, Azure Blob, GCS, IBM COS, Git
- **Type-safe generics** — `Config[T]` with `cfg.Typed()` returning `*T` and full IDE autocomplete
- **6 merge strategies** — replace, merge, append, prepend, intersection, union — with per-path overrides
- **Secret resolution** — `${secret:key}` placeholders from AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, HashiCorp Vault, and OpenBao. The Vault-compatible layer implements nine authentication flows; CI live-tests Token and AppRole against OpenBao, while the other flows have protocol-level tests and require provider-side identity configuration.
- **Config composition** — Hydra-style `_include` and `_defaults` directives with cycle detection
- **Environment resolution** — Recommended named files or a single file with `default` + environment sections, with explicit hybrid mode for migrations
- **Hook system** — 4 types (key, value, condition, global) for value transformation on access
- **Introspection** — `Explain()`, `Layers()`, `Schema()`, source tracking, override history
- **Drift detection** — Diff configs, detect unintended changes, version with rollback
- **Dynamic reloading** — File watching via fsnotify, incremental reload (mtime + SHA256)
- **Observability** — Access metrics, event emission, change callbacks
- **CLI tool** — 13 commands: init, env, load, get, validate, export, diff, debug, explain, plan, lint, docs, migrate
- **Thread-safe** — synchronized Config instances, callback-safe lifecycle events, and concurrency-safe process registries/caches

## Install

Run `go get` from an existing Go module. The CLI installation is independent
and may run from any directory:

```bash
go get github.com/confiify/confii-go@latest
go install github.com/confiify/confii-go/confii@latest
confii --version
```

## Quick Start

Start with an empty Go module, then let the CLI scaffold the project-wide
self-configuration and environment files:

```bash
mkdir my-service
cd my-service
go mod init example.com/my-service
go get github.com/confiify/confii-go@latest
go install github.com/confiify/confii-go/confii@latest
confii --version
confii init
```

Choose **Separate files (recommended)**. The result is immediately loadable:

```text
my-service/
├── .confii.yaml
└── config/
    ├── default.yaml
    ├── development.yaml
    └── production.yaml
```

```go title="main.go"
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

Use the same environment selector in the CLI and the application:

```bash
confii env
confii env list
confii plan
go run .

APP_ENV=production confii plan
APP_ENV=production go run .
```

The full guide explains installation, generated files, safe re-initialization,
typed access, runtime overrides, and the one-file environment alternative.

[:material-arrow-right: Full Quick Start Guide](quickstart.md){ .md-button }
[:material-github: View Examples](https://github.com/confiify/confii-go/tree/main/examples){ .md-button .md-button--primary }
