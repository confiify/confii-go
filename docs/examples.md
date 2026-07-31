# Examples

Confii includes focused runnable examples for individual APIs and workflows.
They live in the
[`examples/`](https://github.com/confiify/confii-go/tree/main/examples)
directory.

If you are creating an application rather than exploring one isolated feature,
start with the [Quick Start](quickstart.md). It installs the CLI, runs
`confii init`, explains the generated project structure, and uses the same
self-configured runtime path recommended for new projects.

For a realistic CRUD application that combines multiple environments,
LocalStack-backed cloud configuration, Vault/OpenBao, introspection, Docker,
and CLI usage, use the companion
[`confiify/confii-go-examples`](https://github.com/confiify/confii-go-examples)
repository.

---

## Running Examples

```bash
cd examples/<name> && go run .
```

For example:

```bash
cd examples/basic && go run .
```

!!! tip "Check each example's directory"
    Most examples include a `config.yaml` or similar file alongside `main.go`. The example reads from these local files, so always `cd` into the example directory before running.

---

## Getting Started

| Example | Description | Key Concepts |
|---------|-------------|--------------|
| [`basic`](https://github.com/confiify/confii-go/tree/main/examples/basic) | Load a YAML file, access values with dot notation | `New`, `Get`, `GetIntOr`, `GetBoolOr` |
| [`typed`](https://github.com/confiify/confii-go/tree/main/examples/typed) | Type-safe `Config[T]` with struct validation | `Config[T]`, `Typed()`, `validate` tags |
| [`builder`](https://github.com/confiify/confii-go/tree/main/examples/builder) | Fluent builder pattern for conditional construction | `NewBuilder`, `AddLoader`, `Build` |
| [`self-config`](https://github.com/confiify/confii-go/tree/main/examples/self-config) | `.confii.yaml` auto-discovery | Self-configuration file |

---

## Loading & Merging

| Example | Description | Key Concepts |
|---------|-------------|--------------|
| [`multi-source`](https://github.com/confiify/confii-go/tree/main/examples/multi-source) | Multiple loaders + environment variables | `WithLoaders`, precedence order |
| [`environment`](https://github.com/confiify/confii-go/tree/main/examples/environment) | Environment-aware config (default + production) | `WithEnv`, `default` section merging |
| [`merge-strategies`](https://github.com/confiify/confii-go/tree/main/examples/merge-strategies) | Per-path merge strategies | `WithMergeStrategyMap`, 7 strategies |
| [`composition`](https://github.com/confiify/confii-go/tree/main/examples/composition) | `_include` and `_defaults` directives | Hydra-style composition, cycle detection |
| [`cloud`](https://github.com/confiify/confii-go/tree/main/examples/cloud) | Cloud loaders and secret stores | S3, SSM, Azure, GCP, Vault |

---

## Processing & Validation

| Example | Description | Key Concepts |
|---------|-------------|--------------|
| [`hooks`](https://github.com/confiify/confii-go/tree/main/examples/hooks) | Key, value, condition, and global hooks | Construction-time hook options, 4 hook types |
| [`validation`](https://github.com/confiify/confii-go/tree/main/examples/validation) | Struct tags + JSON Schema validation | `WithValidateOnLoad`, JSON Schema |
| [`secrets`](https://github.com/confiify/confii-go/tree/main/examples/secrets) | Secret resolution with `${secret:key}` | `SecretResolver`, `DictStore`, caching |
| [`mixed-secrets`](https://github.com/confiify/confii-go/tree/main/examples/mixed-secrets) | Mixed secret backends with environment defaults and explicit routing | `default_provider`, `environment_defaults`, `${secret@provider:key}` |

---

## Runtime & Debugging

| Example | Description | Key Concepts |
|---------|-------------|--------------|
| [`lifecycle`](https://github.com/confiify/confii-go/tree/main/examples/lifecycle) | Reload, freeze, override, change callbacks | `Reload`, `Freeze`, `Override`, `OnChange` |
| [`dynamic-reload`](https://github.com/confiify/confii-go/tree/main/examples/dynamic-reload) | File watching with fsnotify | `WithDynamicReloading`, `StopWatching` |
| [`introspection`](https://github.com/confiify/confii-go/tree/main/examples/introspection) | Explain, Layers, source tracking, debug | `Explain`, `Layers`, `PrintDebugInfo` |
| [`observability`](https://github.com/confiify/confii-go/tree/main/examples/observability) | Metrics and event emission | `EnableObservability`, `EnableEvents` |
| [`versioning`](https://github.com/confiify/confii-go/tree/main/examples/versioning) | Snapshot, compare, and rollback | `EnableVersioning`, `SaveVersion`, `RollbackToVersion` |
| [`diff`](https://github.com/confiify/confii-go/tree/main/examples/diff) | Diff configs and detect drift | `Diff`, `DetectDrift`, `DriftDetector` |
| [`export`](https://github.com/confiify/confii-go/tree/main/examples/export) | Export to JSON/YAML/TOML + doc generation | `Export`, `GenerateDocs` |

---

## Example Walkthroughs

### Basic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    confii "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
)

func main() {
    cfg, err := confii.New[any](confii.WithLoaders(
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

    fmt.Printf("Host: %v, Port: %d, Debug: %v\n", host, port, debug)
    fmt.Printf("All keys: %v\n", cfg.Keys())
}
```

### Type-Safe Config

```go
package main

import (
    "context"
    "fmt"
    "log"

    confii "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
)

type AppConfig struct {
    Database struct {
        Host string `confii:"host" validate:"required,hostname"`
        Port int    `confii:"port" validate:"required,min=1,max=65535"`
    } `confii:"database"`
    App struct {
        Name  string `confii:"name" validate:"required"`
        Debug bool   `confii:"debug"`
    } `confii:"app"`
}

func main() {
    cfg, err := confii.New[AppConfig](confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithValidateOnLoad(true),
    )
    if err != nil {
        log.Fatal(err)
    }

    model, _ := cfg.Typed()
    fmt.Printf("App: %s\n", model.App.Name)
    fmt.Printf("DB:  %s:%d\n", model.Database.Host, model.Database.Port)
}
```

### Introspection

```go
package main

import (
    "context"
    "fmt"
    "log"

    confii "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
)

func main() {
    cfg, err := confii.New[any](confii.WithLoaders(
            loader.NewYAML("base.yaml"),
            loader.NewYAML("prod.yaml"),
        ),
        confii.WithEnv("production"),
        confii.WithDebugMode(true),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Explain where a value came from
    info := cfg.Explain("database.host")
    fmt.Printf("database.host = %v (from %s, overridden %v times)\n",
        info["value"], info["source"], info["override_count"])

    // Show all layers
    fmt.Println("\nLayers:")
    for _, l := range cfg.Layers() {
        fmt.Printf("  %s (%s): %d keys\n",
            l["source"], l["loader_type"], l["key_count"])
    }

    // Print full debug info
    fmt.Println("\nDebug Info:")
    fmt.Print(cfg.PrintDebugInfo(""))
}
```
