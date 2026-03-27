# Configuration Composition

Confii supports Hydra-style configuration composition through `_include`, `_defaults`, and `_merge_strategy` directives. These let you split configuration across multiple files, define base values inline, and control how pieces are assembled.

---

## Overview

Composition directives are special keys in your config files:

| Directive | Purpose |
|---|---|
| `_include` | Import and merge external config files |
| `_defaults` | Provide inline base values (lowest priority) |
| `_merge_strategy` | Hint for merge behavior (removed from output) |

All directive keys are **removed from the final output** -- they never appear in your resolved configuration.

---

## `_include` Directive

The `_include` directive loads external config files and deep-merges their contents on top of the current config. This is how you share common configuration across services or split large configs into manageable pieces.

### Basic Usage

```yaml title="config.yaml"
_include:
  - shared/logging.yaml
  - shared/database.yaml

app:
  name: my-service
  port: 8080
```

```yaml title="shared/logging.yaml"
logging:
  level: info
  format: json
  output: stdout
```

```yaml title="shared/database.yaml"
database:
  driver: postgres
  port: 5432
  pool_size: 10
```

The resolved config will contain `app`, `logging`, and `database` sections merged together.

### Path Resolution

Included file paths are resolved **relative to the source file's directory**:

```text
project/
  config/
    config.yaml          <-- contains _include: ["shared/db.yaml"]
    shared/
      db.yaml            <-- resolved as config/shared/db.yaml
      shared-base.yaml
```

Absolute paths are also supported:

```yaml
_include:
  - /etc/myapp/global.yaml
  - shared/local.yaml
```

### Single File Include

You can include a single file as a string instead of a list:

```yaml
_include: shared/logging.yaml

app:
  name: my-service
```

### Recursive Includes

Included files can themselves contain `_include` directives. Confii processes them recursively:

```yaml title="config.yaml"
_include:
  - shared/base.yaml

app:
  name: my-service
```

```yaml title="shared/base.yaml"
_include:
  - common/logging.yaml
  - common/metrics.yaml

server:
  host: 0.0.0.0
```

```yaml title="shared/common/logging.yaml"
logging:
  level: info
```

!!! note "Include merge order"
    Included files are merged on **top** of the current config. If an included file defines a key that already exists, the included file's value wins. When multiple files are included, they are merged in list order (later files override earlier ones).

---

## `_defaults` Directive

The `_defaults` directive provides base values that go **underneath** the current config. Unlike `_include`, defaults have the lowest priority -- any value in the current file overrides a default.

### Inline String Defaults

Specify simple key-value pairs as `"key: value"` strings:

```yaml title="config.yaml"
_defaults:
  - "timeout: 30"
  - "retry_count: 3"
  - "log_level: info"

timeout: 60  # overrides the default of 30
```

Result:

```yaml
timeout: 60        # from config (overrides default)
retry_count: 3     # from defaults
log_level: info    # from defaults
```

### Map Defaults

Specify structured defaults as maps:

```yaml title="config.yaml"
_defaults:
  - cache:
      driver: memory
      ttl: 300
  - server:
      host: 0.0.0.0
      port: 8080

server:
  port: 9090  # overrides the default
```

Result:

```yaml
cache:
  driver: memory
  ttl: 300
server:
  host: 0.0.0.0    # from defaults
  port: 9090        # from config (overrides default)
```

### Mixing Strings and Maps

You can combine both forms in the same `_defaults` list:

```yaml
_defaults:
  - "timeout: 30"
  - cache:
      driver: memory
  - "debug: false"
```

---

## `_merge_strategy` Key

The `_merge_strategy` key is a metadata hint that can be placed in config files. It is always removed from the final output. Its presence is informational -- actual merge behavior is controlled by the `WithMergeStrategyMap` option in code.

```yaml
_merge_strategy: replace

database:
  host: prod-db
  port: 5432
```

After composition, the output contains only `database` -- the `_merge_strategy` key is stripped.

---

## Cycle Detection and Max Depth

Confii tracks visited file paths and prevents circular includes:

```yaml title="a.yaml"
_include:
  - b.yaml
```

```yaml title="b.yaml"
_include:
  - a.yaml  # circular reference -- silently skipped
```

!!! warning "Maximum depth is 10"
    Recursive includes are limited to a depth of 10 levels. If this limit is exceeded, Confii returns an error: `"composition max depth (10) exceeded at <path>"`. This protects against deeply nested or accidentally recursive configurations.

---

## Directive Key Removal

All three directive keys (`_include`, `_defaults`, `_merge_strategy`) are removed from the final output. Your application code never sees them:

=== "Input"

    ```yaml
    _defaults:
      - "timeout: 30"

    _include:
      - shared/db.yaml

    _merge_strategy: merge

    app:
      name: my-service
    ```

=== "Output"

    ```yaml
    timeout: 30
    app:
      name: my-service
    # Plus whatever shared/db.yaml contributed
    # No _defaults, _include, or _merge_strategy keys
    ```

---

## Complete Example with shared/ Directory

A realistic project structure using composition:

```text
config/
  config.yaml              # main config
  config.production.yaml   # production overrides
  shared/
    logging.yaml           # shared logging config
    database.yaml          # shared database config
    cache.yaml             # shared cache config
    monitoring.yaml        # shared monitoring config
```

```yaml title="config/config.yaml"
_defaults:
  - "environment: development"
  - "debug: true"

_include:
  - shared/logging.yaml
  - shared/database.yaml
  - shared/cache.yaml

app:
  name: order-service
  version: 1.0.0

server:
  host: 0.0.0.0
  port: 8080
```

```yaml title="config/shared/logging.yaml"
logging:
  level: info
  format: json
  output: stdout
  fields:
    service: order-service
```

```yaml title="config/shared/database.yaml"
_include:
  - ../shared/monitoring.yaml  # relative to this file's directory

database:
  driver: postgres
  host: localhost
  port: 5432
  name: orders
  pool_size: 10
  ssl: false
```

```yaml title="config/shared/cache.yaml"
cache:
  driver: redis
  url: redis://localhost:6379
  ttl: 300
  prefix: "orders:"
```

```yaml title="config/shared/monitoring.yaml"
monitoring:
  enabled: true
  endpoint: /metrics
  interval: 30s
```

```yaml title="config/config.production.yaml"
_include:
  - shared/logging.yaml

app:
  debug: false

database:
  host: prod-db.example.com
  pool_size: 50
  ssl: true

cache:
  url: redis://prod-cache.example.com:6379
  ttl: 3600

logging:
  level: warn
```

```go title="main.go"
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    ctx := context.Background()

    loaders := []confii.Loader{
        loader.NewYAML("config/config.yaml"),
    }

    if os.Getenv("APP_ENV") == "production" {
        loaders = append(loaders, loader.NewYAML("config/config.production.yaml"))
    }

    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loaders...),
    )
    if err != nil {
        panic(err)
    }

    fmt.Printf("App: %s\n", cfg.GetStringOr("app.name", ""))
    fmt.Printf("DB Host: %s\n", cfg.GetStringOr("database.host", ""))
    fmt.Printf("Cache URL: %s\n", cfg.GetStringOr("cache.url", ""))
    fmt.Printf("Log Level: %s\n", cfg.GetStringOr("logging.level", ""))
    fmt.Printf("Monitoring: %v\n", cfg.GetBoolOr("monitoring.enabled", false))

    // All keys from composed config
    fmt.Printf("All keys: %v\n", cfg.Keys())
}
```

!!! tip "Composition + Environment Resolution"
    Composition (`_include`/`_defaults`) runs **before** environment resolution. This means your included files can also contain `default`/`production`/etc. sections, and they will be properly resolved after merging. See [Environment Resolution](environment.md) for details.

---

## Supported File Formats

Included files are auto-detected by extension:

| Extension | Format |
|---|---|
| `.yaml`, `.yml` | YAML |
| `.json` | JSON |
| `.toml` | TOML |
| (other) | YAML (default fallback) |
