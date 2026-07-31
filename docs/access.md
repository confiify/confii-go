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

!!! note "Map and slice values are isolated snapshots"
    When `Get` returns a map or slice, Confii returns a deep copy of the
    already-materialized value. Mutating that copy cannot alter live Config
    state, and no transformation hook is rerun during the read.

---

## Getter Methods

### Get

Returns the effective, already-materialized value and an error if the key does not
exist. Constructor-supplied secret references have already been resolved into
the in-memory effective configuration before this call.

```go
val, err := cfg.Get("database.host")
if err != nil {
    // Key not found -- err is a *ConfigError with Code
    // config_not_found; detect it with
    // errors.Is(err, confii.ErrConfigNotFound).
    log.Fatal(err)
}
fmt.Println(val) // "prod-db.example.com"
```

The error includes the requested key and a bounded, sorted list of available
keys, which helps catch typos:

```text
Get: key "databse.host": config key not found [available_keys=[database.host, database.port, ...]]
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

The `GetOr` and typed `Get*Or` helpers return their supplied default when the
underlying read or type conversion fails. Use the corresponding
error-returning getter when failure details must be observed.

### ToDict

Get the entire effective, already-materialized configuration as a `map[string]any`:

```go
raw, err := cfg.ToDict()
if err != nil {
    log.Fatal(err)
}
// map[string]any{
//   "database": map[string]any{
//     "host": "prod-db.example.com",
//     "port": 5432,
//   },
//   ...
// }
```

!!! note "ToDict returns a snapshot"
    `ToDict()` applies the hook pipeline and returns a deep copy. Hook failures
    are returned to the caller. Mutating the returned map does not alter live
    configuration; use `Set()` for intentional runtime changes.

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
    App      AppSection      `confii:"app"`
    Database DatabaseSection `confii:"database"`
    Cache    CacheSection    `confii:"cache"`
}

type AppSection struct {
    Name     string `confii:"name"     validate:"required"`
    Port     int    `confii:"port"     validate:"required,min=1,max=65535"`
    LogLevel string `confii:"log_level"`
}

type DatabaseSection struct {
    Host     string `confii:"host"     validate:"required,hostname"`
    Port     int    `confii:"port"     validate:"required,min=1,max=65535"`
    Name     string `confii:"name"     validate:"required"`
    PoolSize int    `confii:"pool_size" validate:"min=1,max=100"`
    SSL      bool   `confii:"ssl"`
}

type CacheSection struct {
    Driver string `confii:"driver" validate:"required,oneof=memory redis"`
    URL    string `confii:"url"`
    TTL    int    `confii:"ttl"    validate:"min=0"`
}
```

### Creating and Using Config[T]

```go
cfg, err := confii.NewWithContext[AppConfig](ctx,
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
    Hooks already ran while the immutable snapshot was materialized, using full
    paths such as `database.host`. `Typed()` decodes and caches that snapshot.
    Register hooks through constructor options; the hook plan cannot be mutated
    after construction. See [Hook System](hooks.md).

!!! note "Typed fields are ordinary Go fields"
    After `Typed()` returns, expressions such as `model.Database.Host` do not
    execute hooks. The field contains the result of the hook pass performed
    before decoding. Per-access side effects require a Confii getter rather
    than direct struct-field access.

### Struct Tags

Confii owns the mapping directive used by typed configuration and uses the
standard validator directive for validation:

| Tag | Library | Purpose |
|---|---|---|
| `confii` | Confii | Maps configuration keys to Go struct fields |
| `validate` | [go-playground/validator](https://github.com/go-playground/validator) | Validates field values |

The `confii` tag controls how source keys map to Go fields across every loader
format. The `validate` tag defines rules checked by `Typed()` and
`WithValidateOnLoad`. `mapstructure` is an internal decoding dependency, not a
supported public struct-tag directive.

```go
type ServerConfig struct {
    Host     string         `confii:"host" validate:"required,hostname"`
    LogLevel string         `confii:"log_level"`
    Ignored  string         `confii:"-"`
    Extra    map[string]any `confii:",remain"`
}
```

Supported mapping forms are:

| Form | Meaning |
|---|---|
| `confii:"name"` | Bind the field to `name` |
| `confii:"-"` | Ignore the field during typed decoding |
| `confii:",squash"` | Flatten an embedded struct into its parent |
| `confii:",remain"` | Capture otherwise-unused keys in a map field |

Untagged fields continue to match their Go field names case-insensitively. Use
an explicit `confii` tag for snake_case or otherwise renamed keys. See
[Validation](validation.md) for validation rules.

---

## Combining Untyped and Typed Access

Both approaches may be used on the same `Config[T]` instance:

```go
cfg, _ := confii.NewWithContext[AppConfig](ctx,
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

    "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
)

type Config struct {
    App struct {
        Name string `confii:"name" validate:"required"`
        Port int    `confii:"port" validate:"required,min=1024,max=65535"`
    } `confii:"app"`
    Database struct {
        Host string `confii:"host" validate:"required"`
        Port int    `confii:"port" validate:"required"`
    } `confii:"database"`
}

func main() {
    ctx := context.Background()

    cfg, err := confii.NewWithContext[Config](ctx,
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
