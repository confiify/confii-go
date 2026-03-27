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
  <a href="https://github.com/confiify/confii-go/actions/workflows/ci.yaml"><img src="https://github.com/confiify/confii-go/actions/workflows/ci.yaml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/confiify/confii-go"><img src="https://codecov.io/gh/confiify/confii-go/branch/main/graph/badge.svg" alt="Coverage"></a>
  <a href="https://goreportcard.com/report/github.com/confiify/confii-go"><img src="https://goreportcard.com/badge/github.com/confiify/confii-go" alt="Go Report Card"></a>
  <a href="https://securityscorecards.dev/viewer/?uri=github.com/confiify/confii-go"><img src="https://api.securityscorecards.dev/projects/github.com/confiify/confii-go/badge" alt="OpenSSF Scorecard"></a>
  <a href="https://github.com/confiify/confii-go/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

---

Confii loads, merges, validates, and manages configuration from **any source** — YAML, JSON, TOML, INI, .env files, environment variables, HTTP endpoints, and cloud stores — with type-safe generics, secret resolution, source tracking, drift detection, and versioning.

## Features

- **Multi-source loading** — YAML, JSON, TOML, INI, .env, env vars, HTTP, S3, SSM, Azure Blob, GCS, IBM COS, Git
- **Type-safe generics** — `Config[T]` with `cfg.Typed()` returning `*T` and full IDE autocomplete
- **6 merge strategies** — replace, merge, append, prepend, intersection, union — with per-path overrides
- **Secret resolution** — `${secret:key}` placeholders from AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, HashiCorp Vault (9 auth methods)
- **Config composition** — Hydra-style `_include` and `_defaults` directives with cycle detection
- **Environment resolution** — Automatic `default` + `production`/`staging` merging
- **Hook system** — 4 types (key, value, condition, global) for value transformation on access
- **Introspection** — `Explain()`, `Layers()`, `Schema()`, source tracking, override history
- **Drift detection** — Diff configs, detect unintended changes, version with rollback
- **Dynamic reloading** — File watching via fsnotify, incremental reload (mtime + SHA256)
- **Observability** — Access metrics, event emission, change callbacks
- **CLI tool** — 10 commands: load, get, validate, export, diff, debug, explain, lint, docs, migrate
- **Thread-safe** — `sync.RWMutex`, zero global state, safe for concurrent reads

## Install

```bash
go get github.com/confiify/confii-go
```

## Quick Start

=== "Basic"

    ```go
    cfg, err := confii.New[any](context.Background(),
        confii.WithLoaders(
            loader.NewYAML("config.yaml"),
            loader.NewEnvironment("APP"),
        ),
        confii.WithEnv("production"),
    )
    if err != nil {
        log.Fatal(err)
    }

    host, _ := cfg.Get("database.host")
    port := cfg.GetIntOr("database.port", 5432)
    debug := cfg.GetBoolOr("debug", false)
    ```

=== "Type-Safe"

    ```go
    type AppConfig struct {
        Database struct {
            Host string `mapstructure:"host" validate:"required"`
            Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
        } `mapstructure:"database"`
        Debug bool `mapstructure:"debug"`
    }

    cfg, err := confii.New[AppConfig](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithValidateOnLoad(true),
    )

    model, _ := cfg.Typed()
    fmt.Println(model.Database.Host) // IDE autocomplete works
    ```

=== "Builder"

    ```go
    cfg, err := confii.NewBuilder[AppConfig]().
        WithEnv("production").
        AddLoader(loader.NewYAML("base.yaml")).
        AddLoader(loader.NewYAML("prod.yaml")).
        EnableFreezeOnLoad().
        Build(ctx)
    ```

[:material-arrow-right: Full Quick Start Guide](quickstart.md){ .md-button }
[:material-github: View Examples](https://github.com/confiify/confii-go/tree/main/examples){ .md-button .md-button--primary }
