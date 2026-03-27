# Validation

Confii supports two complementary validation approaches: struct tag validation (for Go type safety) and JSON Schema validation (for schema-driven contracts). Both can be used independently or combined.

---

## Struct Tag Validation

Struct tag validation uses [go-playground/validator](https://github.com/go-playground/validator) to enforce rules defined directly on your Go structs. This is the primary validation mechanism when using `Config[T]` with a typed struct.

### Basic Setup

Define validation rules using the `validate` struct tag:

```go
type AppConfig struct {
    App struct {
        Name    string `mapstructure:"name"     validate:"required"`
        Port    int    `mapstructure:"port"     validate:"required,min=1024,max=65535"`
        Version string `mapstructure:"version"  validate:"semver"`
    } `mapstructure:"app"`

    Database struct {
        Host     string `mapstructure:"host"     validate:"required,hostname"`
        Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
        Name     string `mapstructure:"name"     validate:"required,min=1,max=63"`
        User     string `mapstructure:"user"     validate:"required"`
        PoolSize int    `mapstructure:"pool_size" validate:"min=1,max=200"`
        SSL      bool   `mapstructure:"ssl"`
    } `mapstructure:"database"`

    Email struct {
        From string `mapstructure:"from" validate:"required,email"`
        SMTP string `mapstructure:"smtp" validate:"required,hostname"`
        Port int    `mapstructure:"port" validate:"required,oneof=25 465 587"`
    } `mapstructure:"email"`
}
```

### WithValidateOnLoad

Validate immediately when the config is created. If validation fails, `New` returns an error:

```go
cfg, err := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
)
if err != nil {
    // Validation failed -- err contains details
    log.Fatal(err)
}
```

### WithStrictValidation

By default, validation failures on load produce a **warning** log and allow construction to proceed. With strict validation, failures become hard errors:

```go
cfg, err := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
    confii.WithStrictValidation(true), // fail hard on validation errors
)
if err != nil {
    // err is guaranteed to be a validation error, not just a warning
    log.Fatal(err)
}
```

=== "Without Strict (default)"

    ```text
    WARN: validation failed on load: struct validation: ...
    (config is still created and usable)
    ```

=== "With Strict"

    ```text
    ERROR: struct validation: Key: 'AppConfig.Database.Host' ...
    (New returns error, no config created)
    ```

### Manual Validation via Typed()

You can also validate on demand by calling `Typed()`, which decodes the config map into your struct and validates it:

```go
cfg, _ := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    // No WithValidateOnLoad -- validate later
)

// Validate when ready
model, err := cfg.Typed()
if err != nil {
    log.Fatal("config validation failed:", err)
}

fmt.Println(model.Database.Host)
```

---

## Common Validation Tags

The `validate` tag uses [go-playground/validator](https://pkg.go.dev/github.com/go-playground/validator/v10) syntax. Here are the most commonly used tags for configuration:

| Tag | Description | Example |
|---|---|---|
| `required` | Field must be non-zero | `validate:"required"` |
| `min=N` | Minimum value (int) or length (string) | `validate:"min=1"` |
| `max=N` | Maximum value (int) or length (string) | `validate:"max=65535"` |
| `oneof=a b c` | Value must be one of the listed options | `validate:"oneof=debug info warn error"` |
| `hostname` | Valid hostname (RFC 952) | `validate:"hostname"` |
| `email` | Valid email address | `validate:"email"` |
| `url` | Valid URL | `validate:"url"` |
| `ip` | Valid IPv4 or IPv6 address | `validate:"ip"` |
| `cidr` | Valid CIDR notation | `validate:"cidr"` |
| `alphanum` | Alphanumeric characters only | `validate:"alphanum"` |
| `gt=N` | Greater than N | `validate:"gt=0"` |
| `gte=N` | Greater than or equal to N | `validate:"gte=1"` |
| `lt=N` | Less than N | `validate:"lt=100"` |
| `lte=N` | Less than or equal to N | `validate:"lte=65535"` |
| `len=N` | Exact length | `validate:"len=36"` |
| `dir` | Must be an existing directory | `validate:"dir"` |
| `file` | Must be an existing file | `validate:"file"` |
| `semver` | Semantic version string | `validate:"semver"` |

Combine tags with commas for AND logic:

```go
Port int `validate:"required,min=1,max=65535"`
```

Use `|` for OR logic:

```go
Addr string `validate:"ip|hostname"`
```

---

## JSON Schema Validation

For schema-driven validation that is language-agnostic and shareable, use JSON Schema. This is ideal when the schema is maintained separately from the Go code (e.g., in a shared repository or API contract).

### From a Schema File

```go
import "github.com/confiify/confii-go/validate"

v, err := validate.NewJSONSchemaValidatorFromFile("schema.json")
if err != nil {
    log.Fatal(err)
}

err = v.Validate(cfg.ToDict())
if err != nil {
    log.Fatal("Schema validation failed:", err)
}
```

```json title="schema.json"
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["database", "app"],
  "properties": {
    "app": {
      "type": "object",
      "required": ["name", "port"],
      "properties": {
        "name": {
          "type": "string",
          "minLength": 1
        },
        "port": {
          "type": "integer",
          "minimum": 1024,
          "maximum": 65535
        }
      }
    },
    "database": {
      "type": "object",
      "required": ["host", "port"],
      "properties": {
        "host": {
          "type": "string",
          "format": "hostname"
        },
        "port": {
          "type": "integer",
          "minimum": 1,
          "maximum": 65535
        },
        "ssl": {
          "type": "boolean",
          "default": false
        }
      }
    }
  }
}
```

### From a Schema Map

Build the schema programmatically in Go:

```go
import "github.com/confiify/confii-go/validate"

schema := map[string]any{
    "type":     "object",
    "required": []string{"database"},
    "properties": map[string]any{
        "database": map[string]any{
            "type":     "object",
            "required": []string{"host", "port"},
            "properties": map[string]any{
                "host": map[string]any{
                    "type":      "string",
                    "minLength": 1,
                },
                "port": map[string]any{
                    "type":    "integer",
                    "minimum": 1,
                    "maximum": 65535,
                },
            },
        },
    },
}

v, err := validate.NewJSONSchemaValidator(schema)
if err != nil {
    log.Fatal(err)
}

err = v.Validate(cfg.ToDict())
if err != nil {
    log.Fatal(err)
}
```

---

## Combining Struct + Schema Validation

Use both approaches for defense in depth -- struct tags catch type-level issues at the Go layer, while JSON Schema enforces the contract at the data layer:

```go
type AppConfig struct {
    Database struct {
        Host string `mapstructure:"host" validate:"required,hostname"`
        Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
    } `mapstructure:"database"`
}

cfg, err := confii.New[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
    confii.WithStrictValidation(true),
)
if err != nil {
    log.Fatal("Struct validation failed:", err)
}

// Additionally validate against JSON Schema
schemaValidator, err := validate.NewJSONSchemaValidatorFromFile("schema.json")
if err != nil {
    log.Fatal(err)
}

if err := schemaValidator.Validate(cfg.ToDict()); err != nil {
    log.Fatal("Schema validation failed:", err)
}

log.Println("All validations passed")
```

!!! tip "When to use which"
    - **Struct tags**: Best for Go-specific validation that maps directly to your application's type system. Fast, compiled, and IDE-friendly.
    - **JSON Schema**: Best for cross-language contracts, externally maintained schemas, or when you need schema features like `patternProperties`, `additionalProperties`, or `oneOf`/`anyOf`.

---

## Error Handling

### Struct Validation Errors

Struct validation errors from `Typed()` or `WithValidateOnLoad` are wrapped in a `ValidationError`:

```go
model, err := cfg.Typed()
if err != nil {
    // err message includes field-level details:
    // "struct validation: Key: 'AppConfig.Database.Host'
    //  Error:Field validation for 'Host' failed on the 'required' tag"
    fmt.Println(err)
}
```

### JSON Schema Validation Errors

JSON Schema errors include the instance path and error kind:

```go
err := schemaValidator.Validate(cfg.ToDict())
if err != nil {
    // "JSON Schema validation failed: /database/port: minimum;
    //  /database/host: type"
    fmt.Println(err)
}
```

### Validation on Reload

When reloading with `WithReloadValidate(true)`, validation failures cause the reload to **roll back** -- the config reverts to its pre-reload state:

```go
err := cfg.Reload(ctx, confii.WithReloadValidate(true))
if err != nil {
    // Reload failed validation -- config is unchanged
    log.Println("reload rejected:", err)
}
```

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
    "github.com/confiify/confii-go/validate"
)

type ServerConfig struct {
    Server struct {
        Host    string `mapstructure:"host"    validate:"required,ip|hostname"`
        Port    int    `mapstructure:"port"    validate:"required,min=1,max=65535"`
        TLS     bool   `mapstructure:"tls"`
    } `mapstructure:"server"`

    Database struct {
        Host     string `mapstructure:"host"     validate:"required,hostname"`
        Port     int    `mapstructure:"port"     validate:"required,min=1,max=65535"`
        Name     string `mapstructure:"name"     validate:"required,alphanum"`
        MaxConns int    `mapstructure:"max_conns" validate:"min=1,max=500"`
    } `mapstructure:"database"`

    Logging struct {
        Level  string `mapstructure:"level"  validate:"required,oneof=debug info warn error"`
        Format string `mapstructure:"format" validate:"required,oneof=json text"`
    } `mapstructure:"logging"`
}

func main() {
    ctx := context.Background()

    // Step 1: Create config with struct validation
    cfg, err := confii.New[ServerConfig](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
        confii.WithValidateOnLoad(true),
        confii.WithStrictValidation(true),
    )
    if err != nil {
        log.Fatalf("Config validation failed: %v", err)
    }

    // Step 2: Additional JSON Schema validation
    sv, err := validate.NewJSONSchemaValidatorFromFile("schema.json")
    if err != nil {
        log.Fatalf("Failed to load schema: %v", err)
    }
    if err := sv.Validate(cfg.ToDict()); err != nil {
        log.Fatalf("Schema validation failed: %v", err)
    }

    // Step 3: Use validated config
    model, _ := cfg.Typed()
    fmt.Printf("Server: %s:%d (TLS: %v)\n",
        model.Server.Host, model.Server.Port, model.Server.TLS)
    fmt.Printf("Database: %s:%d/%s\n",
        model.Database.Host, model.Database.Port, model.Database.Name)
    fmt.Printf("Logging: %s (%s)\n",
        model.Logging.Level, model.Logging.Format)
}
```

```yaml title="config.yaml"
default:
  server:
    host: 0.0.0.0
    port: 8080
    tls: false
  database:
    host: localhost
    port: 5432
    name: myapp
    max_conns: 10
  logging:
    level: info
    format: json

production:
  server:
    port: 443
    tls: true
  database:
    host: prod-db.example.com
    max_conns: 100
  logging:
    level: warn
```
