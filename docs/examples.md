# Examples

Confii includes 19 runnable examples covering every major feature. All examples are in the [`examples/`](https://github.com/confiify/confii-go/tree/main/examples) directory.

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
| [`merge-strategies`](https://github.com/confiify/confii-go/tree/main/examples/merge-strategies) | Per-path merge strategies | `WithMergeStrategyMap`, 6 strategies |
| [`composition`](https://github.com/confiify/confii-go/tree/main/examples/composition) | `_include` and `_defaults` directives | Hydra-style composition, cycle detection |
| [`cloud`](https://github.com/confiify/confii-go/tree/main/examples/cloud) | Cloud loaders and secret stores | S3, SSM, Azure, GCP, Vault |

---

## Processing & Validation

| Example | Description | Key Concepts |
|---------|-------------|--------------|
| [`hooks`](https://github.com/confiify/confii-go/tree/main/examples/hooks) | Key, value, condition, and global hooks | `HookProcessor`, 4 hook types |
| [`validation`](https://github.com/confiify/confii-go/tree/main/examples/validation) | Struct tags + JSON Schema validation | `WithValidateOnLoad`, JSON Schema |
| [`secrets`](https://github.com/confiify/confii-go/tree/main/examples/secrets) | Secret resolution with `${secret:key}` | `SecretResolver`, `DictStore`, caching |

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

    confii "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
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

    confii "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

type AppConfig struct {
    Database struct {
        Host string `mapstructure:"host" validate:"required,hostname"`
        Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
    } `mapstructure:"database"`
    App struct {
        Name  string `mapstructure:"name" validate:"required"`
        Debug bool   `mapstructure:"debug"`
    } `mapstructure:"app"`
}

func main() {
    cfg, err := confii.New[AppConfig](context.Background(),
        confii.WithLoaders(loader.NewYAML("config.yaml")),
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

    confii "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    cfg, err := confii.New[any](context.Background(),
        confii.WithLoaders(
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
