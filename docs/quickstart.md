# Quick Start

Get up and running with Confii in under 5 minutes. This guide walks you through
creating a configuration file, loading it into your Go application, and accessing
values using the full range of Confii's access methods.

---

## Install Confii

```bash
go get github.com/confiify/confii-go
```

---

## 1. Create a Configuration File

Create a `config.yaml` in your project root:

=== "YAML"

    ```yaml title="config.yaml"
    default:
      app:
        name: my-service
        version: 1.0.0

      server:
        host: localhost
        port: 8080
        debug: true

      database:
        host: localhost
        port: 5432
        name: mydb
        max_connections: 10
        ssl: false

    production:
      server:
        host: 0.0.0.0
        debug: false
      database:
        host: prod-db.example.com
        ssl: true
        max_connections: 100
    ```

=== "JSON"

    ```json title="config.json"
    {
      "default": {
        "app": {
          "name": "my-service",
          "version": "1.0.0"
        },
        "server": {
          "host": "localhost",
          "port": 8080,
          "debug": true
        },
        "database": {
          "host": "localhost",
          "port": 5432,
          "name": "mydb",
          "max_connections": 10,
          "ssl": false
        }
      },
      "production": {
        "server": {
          "host": "0.0.0.0",
          "debug": false
        },
        "database": {
          "host": "prod-db.example.com",
          "ssl": true,
          "max_connections": 100
        }
      }
    }
    ```

=== "TOML"

    ```toml title="config.toml"
    [default.app]
    name = "my-service"
    version = "1.0.0"

    [default.server]
    host = "localhost"
    port = 8080
    debug = true

    [default.database]
    host = "localhost"
    port = 5432
    name = "mydb"
    max_connections = 10
    ssl = false

    [production.server]
    host = "0.0.0.0"
    debug = false

    [production.database]
    host = "prod-db.example.com"
    ssl = true
    max_connections = 100
    ```

!!! tip "Environment sections"
    Confii automatically merges `default` with your active environment section.
    In the example above, setting the environment to `"production"` merges
    `production` on top of `default` -- so `database.name` stays `"mydb"` while
    `database.host` becomes `"prod-db.example.com"`.

---

## 2. Load Configuration with `confii.New`

```go title="main.go"
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    ctx := context.Background()

    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(
            loader.NewYAML("config.yaml"),         // (1)!
            loader.NewEnvironment("APP"),           // (2)!
        ),
        confii.WithEnv("production"),               // (3)!
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Config loaded successfully!")
    _ = cfg
}
```

1. Load from a YAML file on disk.
2. Override with environment variables prefixed `APP_` (e.g. `APP_SERVER__PORT=9090`).
3. Activate the `production` environment section.

!!! note
    `confii.New` is generic. Use `confii.New[any]` for untyped access,
    or `confii.New[AppConfig]` for type-safe struct access (see
    [step 5](#5-type-safe-access-with-typed) below).

---

## 3. Access Values

Confii provides multiple ways to read configuration values. All access methods
use **dot-separated key paths** to navigate nested structures.

### Basic Get

```go
// Get returns (any, error) -- error if the key doesn't exist
host, err := cfg.Get("database.host")
if err != nil {
    log.Fatal(err)
}
fmt.Println("DB Host:", host) // "prod-db.example.com"
```

### Get with Default

```go
// GetOr returns the default value when the key is missing
region := cfg.GetOr("database.region", "us-east-1")
fmt.Println("Region:", region) // "us-east-1" (not in config)
```

### MustGet (for tests)

```go
// MustGet panics if the key is missing -- use only in tests
name := cfg.MustGet("app.name")
fmt.Println("App:", name) // "my-service"
```

---

## 4. Typed Getters with Defaults

Confii provides type-specific getters that return the correct Go type directly.
Each has a companion `*Or` variant that accepts a fallback value.

```go
// Strings
appName, err := cfg.GetString("app.name")          // ("my-service", nil)
appName = cfg.GetStringOr("app.name", "fallback")  // "my-service"

// Integers
port, err := cfg.GetInt("server.port")              // (8080, nil)
port = cfg.GetIntOr("server.port", 3000)            // 8080
maxConn := cfg.GetIntOr("database.max_connections", 5) // 100

// Booleans
debug, err := cfg.GetBool("server.debug")           // (false, nil) -- production
debug = cfg.GetBoolOr("server.debug", true)          // false

// Float64
threshold := cfg.GetFloat64("threshold")             // (0, error) -- key missing
```

!!! warning "Type mismatches"
    If a value cannot be converted to the requested type, typed getters return
    a `ConfigError`. Use the `*Or` variants to provide safe defaults, or enable
    `WithTypeCasting(true)` to auto-convert strings like `"8080"` to `int`.

---

## 5. Type-Safe Access with `Typed()`

For full IDE autocomplete and compile-time safety, define a Go struct and use
`Config[T]` with generics:

```go title="main.go"
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

// Define your configuration struct
type AppConfig struct {
    App struct {
        Name    string `mapstructure:"name" validate:"required"`
        Version string `mapstructure:"version"`
    } `mapstructure:"app"`

    Server struct {
        Host  string `mapstructure:"host" validate:"required"`
        Port  int    `mapstructure:"port" validate:"required,min=1,max=65535"`
        Debug bool   `mapstructure:"debug"`
    } `mapstructure:"server"`

    Database struct {
        Host           string `mapstructure:"host" validate:"required"`
        Port           int    `mapstructure:"port" validate:"required"`
        Name           string `mapstructure:"name" validate:"required"`
        MaxConnections int    `mapstructure:"max_connections"`
        SSL            bool   `mapstructure:"ssl"`
    } `mapstructure:"database"`
}

func main() {
    ctx := context.Background()

    cfg, err := confii.New[AppConfig](ctx,              // (1)!
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
        confii.WithValidateOnLoad(true),                 // (2)!
    )
    if err != nil {
        log.Fatal(err) // validation errors surface here
    }

    // Typed() returns *AppConfig with full autocomplete
    model, err := cfg.Typed()                            // (3)!
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("App:", model.App.Name)
    fmt.Println("DB Host:", model.Database.Host)
    fmt.Println("DB Port:", model.Database.Port)
    fmt.Println("SSL:", model.Database.SSL)
}
```

1. Pass your struct type as the generic parameter.
2. Validates struct tags (`validate:"required"`) immediately after loading.
3. Returns `*AppConfig` -- fully typed, cached after first call.

!!! tip "Mixing typed and untyped access"
    Even with `Config[AppConfig]`, you can still use `Get()`, `GetIntOr()`, and
    all other untyped access methods. `Typed()` is an additional convenience, not
    a replacement.

---

## 6. Builder Pattern

When construction logic is conditional or spans multiple steps, use the fluent
builder API:

```go
cfg, err := confii.NewBuilder[AppConfig]().
    WithEnv("production").
    AddLoader(loader.NewYAML("config/base.yaml")).
    AddLoader(loader.NewYAML("config/prod.yaml")).
    AddLoader(loader.NewEnvironment("APP")).
    EnableDeepMerge().
    EnableFreezeOnLoad().                   // (1)!
    Build(ctx)
```

1. Freezes the config after loading -- any subsequent `Set()` call returns `ErrConfigFrozen`.

The builder supports all the same options as the constructor:

| Builder Method              | Equivalent Constructor Option      |
| --------------------------- | ---------------------------------- |
| `WithEnv(name)`             | `confii.WithEnv(name)`             |
| `AddLoader(l)`              | `confii.WithLoaders(l)`            |
| `AddLoaders(l...)`          | `confii.WithLoaders(l...)`         |
| `EnableDeepMerge()`         | `confii.WithDeepMerge(true)`       |
| `EnableEnvExpander()`       | `confii.WithEnvExpander(true)`     |
| `EnableTypeCasting()`       | `confii.WithTypeCasting(true)`     |
| `EnableFreezeOnLoad()`      | `confii.WithFreezeOnLoad(true)`    |
| `EnableDynamicReloading()`  | `confii.WithDynamicReloading(true)`|
| `EnableDebug()`             | `confii.WithDebugMode(true)`       |
| `WithSchemaValidation(s,b)` | `confii.WithSchema(s)` + `confii.WithValidateOnLoad(true)` + `confii.WithStrictValidation(b)` |

---

## 7. Other Useful Access Methods

```go
// Check if a key exists
if cfg.Has("database.ssl") {
    fmt.Println("SSL setting found")
}

// List all leaf keys
allKeys := cfg.Keys()
fmt.Println("Total keys:", len(allKeys))

// List keys under a prefix
dbKeys := cfg.Keys("database")
// ["database.host", "database.max_connections", "database.name", ...]

// Get the raw map
raw := cfg.ToDict()

// Set a value at runtime
err = cfg.Set("feature.new_ui", true)
```

---

## Next Steps

You now know the basics. Explore these topics to unlock the full power of Confii:

| Topic | What you'll learn |
| --- | --- |
| [Configuration](configuration.md) | Self-config files, all constructor options, builder reference, error policies |
| [Sources](sources.md) | File formats, environment variables, HTTP endpoints, cloud loaders |
| [Merge Strategies](merging.md) | 6 merge strategies with per-path overrides |
| [Hooks & Transformation](hooks.md) | Key/value/condition/global hooks, `${VAR}` expansion |
| [Validation](validation.md) | Struct tags, JSON Schema validation |
| [Secret Management](secrets.md) | `${secret:key}` placeholders, cloud secret stores |
| [Introspection](introspection.md) | `Explain()`, `Layers()`, source tracking, debug reports |
