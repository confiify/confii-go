# Environment Resolution

Confii natively understands environment-specific configuration. Instead of maintaining separate files for each environment, you define a single file with `default` and environment-specific sections. Confii merges them automatically at load time.

---

## How It Works

When you set an active environment (e.g., `"production"`), Confii's `envhandler.Handler` performs a three-step resolution:

1. **Extract the `default` section** as the base configuration.
2. **Extract the active environment section** (e.g., `production`).
3. **Deep-merge the environment section on top of `default`**, so environment-specific values override defaults while inheriting everything else.

```text
default:             production:           resolved (env=production):
  database:            database:             database:
    host: localhost      host: prod-db         host: prod-db       <-- overridden
    port: 5432                                 port: 5432          <-- inherited
    pool_size: 5                               pool_size: 5        <-- inherited
  debug: true          debug: false           debug: false          <-- overridden
```

---

## Config File Structure

A typical environment-aware config file contains a `default` key and one or more environment keys at the top level:

```yaml title="config.yaml"
default:
  app:
    name: my-service
    log_level: info
  database:
    host: localhost
    port: 5432
    pool_size: 5
    ssl: false
  cache:
    driver: memory
    ttl: 300

development:
  app:
    log_level: debug
  database:
    host: localhost

staging:
  database:
    host: staging-db.internal
    ssl: true
  cache:
    driver: redis
    url: redis://staging-cache:6379

production:
  app:
    log_level: warn
  database:
    host: prod-db.example.com
    pool_size: 20
    ssl: true
  cache:
    driver: redis
    url: redis://prod-cache.example.com:6379
    ttl: 3600
```

!!! note "Top-level keys are environment names"
    Any top-level key whose value is a map is treated as a potential environment section. Confii does not restrict which environment names you use -- `default`, `production`, `staging`, `development`, `testing`, `qa`, or any custom name all work.

---

## Setting the Active Environment

### WithEnv() -- Explicit Environment

Set the environment directly in code. This is the most common approach for applications that know their environment at startup:

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("production"),
)

// Resolved values:
host, _ := cfg.Get("database.host")    // "prod-db.example.com"
port := cfg.GetIntOr("database.port", 0) // 5432 (inherited from default)
ssl, _ := cfg.GetBool("database.ssl")  // true (overridden by production)
```

### WithEnvSwitcher() -- From OS Environment Variable

Read the environment name from an OS environment variable at runtime. This is ideal for container deployments where the environment is injected:

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnvSwitcher("APP_ENV"),  // reads os.Getenv("APP_ENV")
)
```

```bash
# At runtime:
APP_ENV=staging ./myapp
```

!!! tip "Priority: WithEnv() wins over WithEnvSwitcher()"
    If both `WithEnv()` and `WithEnvSwitcher()` are set, the `WithEnvSwitcher()` value only applies when `WithEnv()` was **not** explicitly set. The resolution order is: **explicit `WithEnv()` > `WithEnvSwitcher()` OS variable > self-config file `default_environment` > empty string**.

### Self-Config File

You can also set a default environment in a `.confii.yaml` file:

```yaml title=".confii.yaml"
default_environment: development
```

This applies with the lowest priority -- any explicit code option overrides it.

---

## Inheritance Behavior

Environment resolution uses deep merge, meaning:

- **Scalar values** in the environment section replace the default.
- **Nested maps** are recursively merged -- environment-specific keys override, but missing keys are inherited from `default`.
- **Lists** are replaced entirely (not appended).

=== "Config File"

    ```yaml
    default:
      server:
        host: 0.0.0.0
        port: 8080
        timeouts:
          read: 30s
          write: 30s
          idle: 120s
      features:
        - logging
        - metrics

    production:
      server:
        port: 443
        timeouts:
          idle: 300s
      features:
        - logging
        - metrics
        - tracing
    ```

=== "Resolved (production)"

    ```yaml
    server:
      host: 0.0.0.0       # inherited from default
      port: 443            # overridden by production
      timeouts:
        read: 30s          # inherited from default
        write: 30s         # inherited from default
        idle: 300s         # overridden by production
    features:              # replaced entirely by production
      - logging
      - metrics
      - tracing
    ```

!!! warning "Lists are replaced, not merged"
    When an environment section provides a list value, it **replaces** the entire list from `default`. If you need list merging behavior, use [merge strategies](merging.md) with `Append` or `Prepend`.

---

## What Happens When an Environment Is Not Found

If the requested environment does not exist as a top-level key in the config:

1. Confii logs a **warning** with the requested environment name and the list of available environments.
2. The resolved config falls back to the **`default` section only**.
3. No error is returned -- the application continues with defaults.

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("canary"),  // not defined in config.yaml
)
// Warning: "environment not found in config, using defaults"
//          env="canary", available=["development", "staging", "production"]

// All values come from the "default" section:
host, _ := cfg.Get("database.host")  // "localhost"
```

!!! tip "No `default` section and no matching environment"
    If the config has **neither** a `default` key nor the requested environment key, Confii treats it as a flat (non-environment-structured) config and returns the entire map as-is. This lets you use the same API for both environment-aware and simple flat configs.

---

## Complete Example with Multiple Environments

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

    // Determine environment from APP_ENV, default to "development"
    env := os.Getenv("APP_ENV")
    if env == "" {
        env = "development"
    }

    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv(env),
    )
    if err != nil {
        panic(err)
    }

    fmt.Printf("Environment: %s\n", cfg.Env())
    fmt.Printf("Database host: %s\n", cfg.GetStringOr("database.host", "unknown"))
    fmt.Printf("Database port: %d\n", cfg.GetIntOr("database.port", 5432))
    fmt.Printf("Database SSL: %v\n", cfg.GetBoolOr("database.ssl", false))
    fmt.Printf("Cache driver: %s\n", cfg.GetStringOr("cache.driver", "memory"))
    fmt.Printf("Log level: %s\n", cfg.GetStringOr("app.log_level", "info"))
}
```

```yaml title="config.yaml"
default:
  app:
    log_level: info
  database:
    host: localhost
    port: 5432
    ssl: false
  cache:
    driver: memory

development:
  app:
    log_level: debug

staging:
  database:
    host: staging-db.internal
    ssl: true
  cache:
    driver: redis

production:
  app:
    log_level: warn
  database:
    host: prod-db.example.com
    ssl: true
  cache:
    driver: redis
```

Running with different environments:

```bash
APP_ENV=development go run .
# Database host: localhost, SSL: false, Log level: debug

APP_ENV=staging go run .
# Database host: staging-db.internal, SSL: true, Log level: info

APP_ENV=production go run .
# Database host: prod-db.example.com, SSL: true, Log level: warn
```

---

## Combining with Multiple Loaders

Environment resolution happens **after** all loaders are merged. This means you can combine multiple config files and environment variables, and the environment section extraction applies to the final merged result:

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("base.yaml"),        // base config with default/production sections
        loader.NewYAML("overrides.yaml"),    // additional overrides (also with sections)
        loader.NewEnvironment("APP"),        // env vars override everything
    ),
    confii.WithEnv("production"),
)
```

The processing pipeline is:

1. Load `base.yaml`, `overrides.yaml`, and environment variables.
2. Deep-merge them in order (later loaders override earlier ones).
3. Extract `default` + `production` from the merged result.
4. Deep-merge `production` on top of `default`.
5. Return the resolved config.
