# Validation

Confii supports three complementary validation approaches: struct tag
validation for Go type safety, JSON Schema validation for schema-driven
contracts, and application-defined validators for domain rules. They can be
used independently or combined in one transactional validation plan.

![Confii configuration startup flow](assets/configuration-flow.svg)

---

## Struct Tag Validation

Struct tag validation uses [go-playground/validator](https://github.com/go-playground/validator) to enforce rules defined directly on your Go structs. This is the primary validation mechanism when using `Config[T]` with a typed struct.

### Basic Setup

Use `confii` to map configuration keys to fields and define validation rules
with the independent `validate` struct tag:

```go
type AppConfig struct {
    App struct {
        Name    string `confii:"name"     validate:"required"`
        Port    int    `confii:"port"     validate:"required,min=1024,max=65535"`
        Version string `confii:"version"  validate:"semver"`
    } `confii:"app"`

    Database struct {
        Host     string `confii:"host"     validate:"required,hostname"`
        Port     int    `confii:"port"     validate:"required,min=1,max=65535"`
        Name     string `confii:"name"     validate:"required,min=1,max=63"`
        User     string `confii:"user"     validate:"required"`
        PoolSize int    `confii:"pool_size" validate:"min=1,max=200"`
        SSL      bool   `confii:"ssl"`
    } `confii:"database"`

    Email struct {
        From string `confii:"from" validate:"required,email"`
        SMTP string `confii:"smtp" validate:"required,hostname"`
        Port int    `confii:"port" validate:"required,oneof=25 465 587"`
    } `confii:"email"`
}
```

### WithValidateOnLoad

Validate immediately when the config is created. If validation fails, `New` returns an error:

```go
cfg, err := confii.NewWithContext[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
)
if err != nil {
    // Validation failed -- err contains details
    log.Fatal(err)
}
```

### WithStrictValidation

Strict typed validation is enabled by default. A typed validation failure
therefore rejects construction or a candidate runtime change. Set strict
validation to false only when typed-tag violations should be logged while the
snapshot is still published:

```go
cfg, err := confii.NewWithContext[AppConfig](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidateOnLoad(true),
    confii.WithStrictValidation(false), // warn for typed-tag violations
)
if err != nil {
    // err is guaranteed to be a validation error, not just a warning
    log.Fatal(err)
}
```

=== "Non-strict"

    ```text
    WARN: validation failed on load: struct validation: ...
    (config is still created and usable)
    ```

=== "Strict (default)"

    ```text
    ERROR: struct validation: Key: 'AppConfig.Database.Host' ...
    (New returns error, no config created)
    ```

JSON Schema and custom-validator failures are always fatal when validation is
enabled. `WithStrictValidation(false)` affects only typed `validate` tags.

### Manual Validation via Typed()

`Typed()` performs on-demand decoding and validation:

```go
cfg, _ := confii.NewWithContext[AppConfig](ctx,
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
import "github.com/confiify/confii-go/v2/validate"

v, err := validate.NewJSONSchemaValidatorFromFile("schema.json")
if err != nil {
    log.Fatal(err)
}

snapshot, err := cfg.ToDict()
if err != nil {
    log.Fatal(err)
}
err = v.Validate(snapshot)
if err != nil {
    log.Fatal("Schema validation failed:", err)
}
```

---

## Application-defined validators

Implement `confii.Validator` for invariants that cannot be expressed cleanly
with struct tags or JSON Schema, and register it before construction:

```go
type deploymentValidator struct{}

func (deploymentValidator) Validate(data map[string]any) error {
    environment, _ := data["environment"].(string)
    debug, _ := data["debug"].(bool)
    if environment == "production" && debug {
        return errors.New("debug must be disabled in production")
    }
    return nil
}

cfg, err := confii.New[AppConfig](
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithValidator(deploymentValidator{}),
)
```

Registering a custom validator enables validation. Custom validators run after
JSON Schema validation and before typed-struct validation, in registration
order. Confii gives each validator an independent copy of the candidate, so
accidental mutation cannot alter the snapshot. An error rejects initial
construction, reload, extension, mutation, override, or secret refresh without
publishing a partial configuration.

The fluent builder provides the same extension point:

```go
cfg, err := confii.NewBuilder[AppConfig]().
    AddLoader(loader.NewYAML("config.yaml")).
    WithValidator(deploymentValidator{}).
    Build()
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
import "github.com/confiify/confii-go/v2/validate"

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

snapshot, err := cfg.ToDict()
if err != nil {
    log.Fatal(err)
}
err = v.Validate(snapshot)
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
        Host string `confii:"host" validate:"required,hostname"`
        Port int    `confii:"port" validate:"required,min=1,max=65535"`
    } `confii:"database"`
}

cfg, err := confii.NewWithContext[AppConfig](ctx,
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

snapshot, err := cfg.ToDict()
if err != nil {
    log.Fatal("Configuration hooks failed:", err)
}
if err := schemaValidator.Validate(snapshot); err != nil {
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
snapshot, err := cfg.ToDict()
if err != nil {
    fmt.Println(err)
    return
}
err = schemaValidator.Validate(snapshot)
if err != nil {
    // "JSON Schema validation failed: /database/port: minimum;
    //  /database/host: type"
    fmt.Println(err)
}
```

### Validation on Reload

When reloading with `WithReloadValidate(true)`, validation failures cause the reload to **roll back** -- the config reverts to its pre-reload state:

```go
err := cfg.ReloadWithContext(ctx, confii.WithReloadValidate(true))
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

    "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
    "github.com/confiify/confii-go/v2/validate"
)

type ServerConfig struct {
    Server struct {
        Host    string `confii:"host"    validate:"required,ip|hostname"`
        Port    int    `confii:"port"    validate:"required,min=1,max=65535"`
        TLS     bool   `confii:"tls"`
    } `confii:"server"`

    Database struct {
        Host     string `confii:"host"     validate:"required,hostname"`
        Port     int    `confii:"port"     validate:"required,min=1,max=65535"`
        Name     string `confii:"name"     validate:"required,alphanum"`
        MaxConns int    `confii:"max_conns" validate:"min=1,max=500"`
    } `confii:"database"`

    Logging struct {
        Level  string `confii:"level"  validate:"required,oneof=debug info warn error"`
        Format string `confii:"format" validate:"required,oneof=json text"`
    } `confii:"logging"`
}

func main() {
    ctx := context.Background()

    // Create configuration with struct validation.
    cfg, err := confii.NewWithContext[ServerConfig](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
        confii.WithValidateOnLoad(true),
        confii.WithStrictValidation(true),
    )
    if err != nil {
        log.Fatalf("Config validation failed: %v", err)
    }

    // Apply additional JSON Schema validation.
    sv, err := validate.NewJSONSchemaValidatorFromFile("schema.json")
    if err != nil {
        log.Fatalf("Failed to load schema: %v", err)
    }
    snapshot, err := cfg.ToDict()
    if err != nil {
        log.Fatalf("Configuration hooks failed: %v", err)
    }
    if err := sv.Validate(snapshot); err != nil {
        log.Fatalf("Schema validation failed: %v", err)
    }

    // Use the validated typed configuration.
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
