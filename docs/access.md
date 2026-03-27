# Accessing Values

Once a `Config[T]` instance is created, Confii provides multiple ways to read and write configuration values -- from untyped dot-notation access to fully type-safe generics.

---

## Dot-Notation Key Paths

All access methods use dot-separated key paths to navigate nested configuration maps:

```yaml
database:
  primary:
    host: prod-db.example.com
    port: 5432
  replicas:
    - host: replica-1.example.com
    - host: replica-2.example.com
```

```go
cfg.Get("database.primary.host")  // "prod-db.example.com"
cfg.Get("database.primary.port")  // 5432
cfg.Get("database.primary")       // map[string]any{"host": ..., "port": ...}
```

!!! note "Maps are returned without hook processing"
    When `Get` returns a map (non-leaf value), hooks are **not** applied. Hooks only transform leaf (scalar) values. This prevents unintended transformations on intermediate map nodes.

---

## Getter Methods

### Get

Returns the raw value and an error if the key does not exist.

```go
val, err := cfg.Get("database.host")
if err != nil {
    // Key not found -- err is a *NotFoundError with suggestions
    log.Fatal(err)
}
fmt.Println(val) // "prod-db.example.com"
```

The error includes the requested key and a list of available keys, which helps catch typos:

```text
key "databse.host" not found; available keys: [database.host, database.port, ...]
```

### GetOr

Returns a default value instead of an error when the key is missing:

```go
host := cfg.GetOr("database.host", "localhost")
debug := cfg.GetOr("debug", false)
```

### MustGet

Panics on error. Intended for tests where missing config is a hard failure:

```go
func TestConfig(t *testing.T) {
    host := cfg.MustGet("database.host").(string)
    assert.Equal(t, "prod-db.example.com", host)
}
```

!!! warning "Do not use MustGet in production code"
    `MustGet` calls `panic()` if the key is not found. Use `Get` or `GetOr` in production.

---

## Typed Getters

Typed getters perform type assertion and return the appropriate Go type:

### GetString / GetStringOr

```go
name, err := cfg.GetString("app.name")          // (string, error)
name := cfg.GetStringOr("app.name", "default")  // string with fallback
```

Non-string values are converted via `fmt.Sprintf("%v", val)`, so integers and booleans work too.

### GetInt / GetIntOr

```go
port, err := cfg.GetInt("database.port")       // (int, error)
port := cfg.GetIntOr("database.port", 5432)    // int with fallback
```

Handles `int`, `int64`, and `float64` source types. Returns a `ConfigError` for incompatible types.

### GetBool / GetBoolOr

```go
debug, err := cfg.GetBool("debug")             // (bool, error)
debug := cfg.GetBoolOr("debug", false)         // bool with fallback
```

!!! tip "Automatic type casting"
    If your config values come from environment variables (which are always strings), enable `WithTypeCasting(true)` to automatically convert `"true"`, `"false"`, `"1"`, `"0"`, and numeric strings to their proper Go types. See [Hooks](hooks.md) for details.

### GetFloat64

```go
threshold, err := cfg.GetFloat64("threshold")  // (float64, error)
```

Handles `float64`, `int`, and `int64` source types.

---

## Existence and Enumeration

### Has

Check if a key exists without retrieving the value:

```go
if cfg.Has("database.host") {
    // key exists
}
```

### Keys

List all leaf key paths, optionally filtered by prefix:

```go
// All keys
allKeys := cfg.Keys()
// ["app.name", "app.port", "database.host", "database.port", ...]

// Keys under a prefix
dbKeys := cfg.Keys("database")
// ["database.host", "database.port", "database.pool_size"]
```

Keys are returned sorted alphabetically.

### ToDict

Get the entire configuration as a raw `map[string]any`:

```go
raw := cfg.ToDict()
// map[string]any{
//   "database": map[string]any{
//     "host": "prod-db.example.com",
//     "port": 5432,
//   },
//   ...
// }
```

!!! note "ToDict returns the live map"
    `ToDict()` returns a reference to the internal map. Modifying it directly is not recommended -- use `Set()` instead.

---

## Setting Values

### Set

Set a value by dot-separated key path. Thread-safe and respects frozen state:

```go
err := cfg.Set("database.host", "new-host.example.com")
if err != nil {
    // ErrConfigFrozen if config is frozen
}
```

### WithOverride

Control whether existing keys can be overwritten:

```go
// Default: overwrite existing keys
cfg.Set("database.host", "new-host")

// Prevent overwriting -- error if key exists
err := cfg.Set("database.host", "another-host", confii.WithOverride(false))
// err: key "database.host" already exists (override=false)

// Safe for adding new keys
cfg.Set("database.new_option", "value", confii.WithOverride(false)) // works
```

### Override (Scoped)

Temporarily override values with automatic rollback. Ideal for tests:

```go
restore, err := cfg.Override(map[string]any{
    "database.host": "test-db",
    "database.port": 5433,
})
defer restore()  // reverts all changes

// Within this scope, database.host = "test-db"
host, _ := cfg.Get("database.host") // "test-db"
```

`Override` even works on frozen configs -- it temporarily unfreezes, applies overrides, and the `restore` function re-freezes.

---

## Typed Access with Config[T]

For full type safety with IDE autocomplete, use `Config[T]` with a struct type parameter.

### Defining the Struct

```go
type AppConfig struct {
    App      AppSection      `mapstructure:"app"`
    Database DatabaseSection `mapstructure:"database"`
    Cache    CacheSection    `mapstructure:"cache"`
}

type AppSection struct {
    Name     string `mapstructure:"name"     validate:"required"`
    Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
    LogLevel string `mapstructure:"log_level"`
}

type DatabaseSection struct {
    Host     string `mapstructure:"host"     validate:"required,hostname"`
    Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
    Name     string `mapstructure:"name"     validate:"required"`
    PoolSize int    `mapstructure:"pool_size" validate:"min=1,max=100"`
    SSL      bool   `mapstructure:"ssl"`
}

type CacheSection struct {
    Driver string `mapstructure:"driver" validate:"required,oneof=memory redis"`
    URL    string `mapstructure:"url"`
    TTL    int    `mapstructure:"ttl"    validate:"min=0"`
}
```

### Creating and Using Config[T]

```go
cfg, err := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
)
if err != nil {
    log.Fatal(err) // includes validation errors
}

// Type-safe access with IDE autocomplete
model, err := cfg.Typed()
if err != nil {
    log.Fatal(err)
}

fmt.Println(model.App.Name)         // string
fmt.Println(model.Database.Host)    // string
fmt.Println(model.Database.Port)    // int
fmt.Println(model.Cache.Driver)     // string
```

!!! tip "Typed() caches the result"
    The decoded and validated struct is cached internally. Subsequent calls to `Typed()` return the cached value unless the config has been modified (via `Set`, `Reload`, or `Override`).

### Struct Tags

Confii uses two struct tag systems:

| Tag | Library | Purpose |
|---|---|---|
| `mapstructure` | [mitchellh/mapstructure](https://github.com/mitchellh/mapstructure) | Maps config keys to struct fields |
| `validate` | [go-playground/validator](https://github.com/go-playground/validator) | Validates field values |

The `mapstructure` tag controls how YAML/JSON keys map to Go struct fields. The `validate` tag defines validation rules checked by `Typed()` and `WithValidateOnLoad`. See [Validation](validation.md) for details.

---

## Combining Untyped and Typed Access

You can use both approaches on the same `Config[T]` instance:

```go
cfg, _ := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
)

// Untyped access (works for any key, including dynamic ones)
host, _ := cfg.Get("database.host")
port := cfg.GetIntOr("database.port", 5432)

// Typed access (only works for fields defined in AppConfig)
model, _ := cfg.Typed()
fmt.Println(model.Database.Host)

// Check all available keys
keys := cfg.Keys()
fmt.Println(keys)
```

!!! note "Config[any] for fully untyped usage"
    If you don't need typed access, use `Config[any]` as the type parameter. `Typed()` will still work but returns `*any`, which is less useful. The untyped getter methods (`Get`, `GetString`, etc.) work identically regardless of the type parameter.

---

## Complete Example

```go title="main.go"
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

type Config struct {
    App struct {
        Name string `mapstructure:"name" validate:"required"`
        Port int    `mapstructure:"port" validate:"required,min=1024,max=65535"`
    } `mapstructure:"app"`
    Database struct {
        Host string `mapstructure:"host" validate:"required"`
        Port int    `mapstructure:"port" validate:"required"`
    } `mapstructure:"database"`
}

func main() {
    ctx := context.Background()

    cfg, err := confii.New[Config](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
        confii.WithValidateOnLoad(true),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Untyped access
    fmt.Println("Host:", cfg.GetStringOr("database.host", "localhost"))
    fmt.Println("Port:", cfg.GetIntOr("database.port", 5432))
    fmt.Println("Has SSL:", cfg.Has("database.ssl"))

    // Typed access
    model, _ := cfg.Typed()
    fmt.Println("App:", model.App.Name)
    fmt.Println("DB:", model.Database.Host)

    // Enumerate keys
    for _, key := range cfg.Keys("database") {
        val, _ := cfg.Get(key)
        fmt.Printf("  %s = %v\n", key, val)
    }

    // Scoped override for testing
    restore, _ := cfg.Override(map[string]any{
        "database.host": "test-db",
    })
    fmt.Println("Override:", cfg.GetStringOr("database.host", ""))
    restore()
    fmt.Println("Restored:", cfg.GetStringOr("database.host", ""))
}
```
