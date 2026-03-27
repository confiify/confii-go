# Merge Strategies

When Confii loads configuration from multiple sources, it merges them together. By default, later loaders override earlier ones using deep merge. Confii provides 6 merge strategies and lets you assign different strategies to different config paths.

---

## Deep Merge vs Shallow Merge

The `WithDeepMerge` option controls the global merge behavior:

=== "Deep Merge (default)"

    Nested maps are recursively merged. Only leaf values are overridden.

    ```go
    confii.WithDeepMerge(true) // this is the default
    ```

    ```text
    Base:                    Overlay:                 Result:
    database:                database:                database:
      host: localhost          host: prod-db            host: prod-db    <-- overridden
      port: 5432                                        port: 5432       <-- preserved
      options:                 options:                 options:
        timeout: 30              ssl: true                timeout: 30    <-- preserved
                                                          ssl: true      <-- added
    ```

=== "Shallow Merge"

    Only top-level keys are merged. Nested maps are replaced entirely.

    ```go
    confii.WithDeepMerge(false)
    ```

    ```text
    Base:                    Overlay:                 Result:
    database:                database:                database:
      host: localhost          host: prod-db            host: prod-db    <-- replaced
      port: 5432                                                         <-- LOST
      options:                 options:                 options:
        timeout: 30              ssl: true                ssl: true      <-- replaced
                                                                         <-- timeout LOST
    ```

!!! warning "Shallow merge loses nested keys"
    With shallow merge disabled, the entire `database` map from the overlay replaces the base. Keys like `port` and `options.timeout` that only exist in the base are lost.

---

## The 6 Merge Strategies

For fine-grained control, use `WithMergeStrategyOption` (global default) and `WithMergeStrategyMap` (per-path overrides). These activate the `AdvancedMerger` which supports all 6 strategies.

### Replace

Overwrites the base value entirely with the overlay value. No merging occurs.

```go
confii.WithMergeStrategyOption(confii.StrategyReplace)
```

=== "Base"

    ```yaml
    database:
      host: localhost
      port: 5432
      pool_size: 5
    ```

=== "Overlay"

    ```yaml
    database:
      host: prod-db
    ```

=== "Result"

    ```yaml
    database:
      host: prod-db
    # port and pool_size are gone
    ```

---

### Merge (Deep Merge)

Recursively deep-merges nested maps. Base keys not in the overlay are preserved. This is the default strategy.

```go
confii.WithMergeStrategyOption(confii.StrategyMerge)
```

=== "Base"

    ```yaml
    database:
      host: localhost
      port: 5432
      pool_size: 5
    ```

=== "Overlay"

    ```yaml
    database:
      host: prod-db
      ssl: true
    ```

=== "Result"

    ```yaml
    database:
      host: prod-db     # overridden
      port: 5432        # preserved
      pool_size: 5      # preserved
      ssl: true         # added
    ```

---

### Append

Appends overlay list items after base list items. Non-list values are wrapped in a single-element list.

```go
confii.WithMergeStrategyOption(confii.StrategyAppend)
```

=== "Base"

    ```yaml
    plugins:
      - auth
      - logging
    ```

=== "Overlay"

    ```yaml
    plugins:
      - metrics
      - tracing
    ```

=== "Result"

    ```yaml
    plugins:
      - auth       # from base
      - logging    # from base
      - metrics    # appended from overlay
      - tracing    # appended from overlay
    ```

---

### Prepend

Inserts overlay list items before base list items.

```go
confii.WithMergeStrategyOption(confii.StrategyPrepend)
```

=== "Base"

    ```yaml
    middleware:
      - cors
      - compress
    ```

=== "Overlay"

    ```yaml
    middleware:
      - rate-limit
    ```

=== "Result"

    ```yaml
    middleware:
      - rate-limit  # prepended from overlay
      - cors        # from base
      - compress    # from base
    ```

---

### Intersection

Keeps only keys that exist in **both** the base and the overlay. For nested maps, the intersection is applied recursively. For scalar values, the base value is kept only if it equals the overlay value.

```go
confii.WithMergeStrategyOption(confii.StrategyIntersection)
```

=== "Base"

    ```yaml
    database:
      host: localhost
      port: 5432
    cache:
      driver: redis
    logging:
      level: debug
    ```

=== "Overlay"

    ```yaml
    database:
      host: prod-db
      ssl: true
    cache:
      driver: redis
    ```

=== "Result"

    ```yaml
    database:
      host: localhost   # key in both, but values differ -> nil
      # port: missing from overlay -> removed
      # ssl: missing from base -> removed
    cache:
      driver: redis     # key in both, values equal -> kept
    # logging: missing from overlay -> removed
    ```

!!! note "Intersection is strict"
    Scalar values are only kept when they are equal in both maps. If you want to keep the overlay value for common keys, use `Union` instead.

---

### Union

Keeps all keys from both maps. Common keys are deep-merged with the overlay taking precedence.

```go
confii.WithMergeStrategyOption(confii.StrategyUnion)
```

=== "Base"

    ```yaml
    database:
      host: localhost
      port: 5432
    logging:
      level: debug
    ```

=== "Overlay"

    ```yaml
    database:
      host: prod-db
      ssl: true
    cache:
      driver: redis
    ```

=== "Result"

    ```yaml
    database:
      host: prod-db     # overlay wins
      port: 5432        # from base (not in overlay)
      ssl: true         # from overlay (not in base)
    logging:
      level: debug      # only in base -> kept
    cache:
      driver: redis     # only in overlay -> kept
    ```

---

## Per-Path Strategy Overrides

The real power comes from assigning different strategies to different config paths. Use `WithMergeStrategyMap` to override the strategy for specific dot-separated paths:

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("base.yaml"),
        loader.NewYAML("override.yaml"),
    ),
    confii.WithMergeStrategyOption(confii.StrategyMerge),  // global default
    confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
        "database":   confii.StrategyReplace,      // replace entire database section
        "features":   confii.StrategyAppend,        // append new features
        "middleware":  confii.StrategyPrepend,       // prepend middleware
        "allowed_ips": confii.StrategyUnion,         // union of all IPs
    }),
)
```

=== "base.yaml"

    ```yaml
    database:
      host: localhost
      port: 5432
      pool_size: 5
    features:
      - auth
      - logging
    middleware:
      - cors
      - compress
    server:
      host: 0.0.0.0
      port: 8080
    ```

=== "override.yaml"

    ```yaml
    database:
      host: prod-db
    features:
      - metrics
    middleware:
      - rate-limit
    server:
      port: 443
    ```

=== "Result"

    ```yaml
    database:                # StrategyReplace: replaced entirely
      host: prod-db
    features:                # StrategyAppend: overlay appended
      - auth
      - logging
      - metrics
    middleware:               # StrategyPrepend: overlay prepended
      - rate-limit
      - cors
      - compress
    server:                  # StrategyMerge (default): deep merged
      host: 0.0.0.0
      port: 443
    ```

!!! tip "Path inheritance"
    Strategy resolution uses the **most specific matching path**. If you set a strategy for `"database"`, it also applies to `"database.options"` and any deeper paths -- unless a more specific path like `"database.options"` has its own strategy.

---

## How Later Loaders Override Earlier Ones

Loaders are processed in the order they are passed to `WithLoaders`. Each subsequent loader's output is merged on top of the accumulated result:

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("defaults.yaml"),    // loaded first  (lowest priority)
        loader.NewYAML("app.yaml"),         // merged on top
        loader.NewYAML("local.yaml"),       // merged on top
        loader.NewEnvironment("APP"),       // merged last   (highest priority)
    ),
)
```

```text
Step 1: result = defaults.yaml
Step 2: result = merge(result, app.yaml)
Step 3: result = merge(result, local.yaml)
Step 4: result = merge(result, env_vars)
```

!!! note "Environment resolution happens after merging"
    All loaders are merged first, then [environment resolution](environment.md) extracts the `default` + active environment sections from the final merged map.

---

## Complete Example

```go title="main.go"
package main

import (
    "context"
    "fmt"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    ctx := context.Background()

    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(
            loader.NewYAML("base.yaml"),
            loader.NewYAML("production.yaml"),
        ),
        confii.WithMergeStrategyOption(confii.StrategyMerge),
        confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
            "database":  confii.StrategyReplace,
            "features":  confii.StrategyAppend,
            "middleware": confii.StrategyPrepend,
        }),
    )
    if err != nil {
        panic(err)
    }

    // Database was fully replaced by production.yaml
    fmt.Println(cfg.GetStringOr("database.host", ""))

    // Features from base.yaml + production.yaml appended
    features, _ := cfg.Get("features")
    fmt.Println(features)

    // All other sections use deep merge
    fmt.Println(cfg.GetIntOr("server.port", 8080))
}
```

---

## Strategy Reference

| Strategy | Constant | Maps | Lists | Scalars |
|---|---|---|---|---|
| Replace | `confii.StrategyReplace` | Overlay replaces base | Overlay replaces base | Overlay replaces base |
| Merge | `confii.StrategyMerge` | Recursive deep merge | Overlay replaces base | Overlay replaces base |
| Append | `confii.StrategyAppend` | Overlay replaces base | Base + Overlay | Wrapped in list, then appended |
| Prepend | `confii.StrategyPrepend` | Overlay replaces base | Overlay + Base | Wrapped in list, then prepended |
| Intersection | `confii.StrategyIntersection` | Keep common keys only | N/A (equality check) | Keep if equal |
| Union | `confii.StrategyUnion` | Deep merge all keys | Overlay replaces base | Overlay replaces base |
