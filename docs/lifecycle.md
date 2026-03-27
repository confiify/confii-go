# Lifecycle Management

Confii provides a full set of lifecycle operations for managing configuration at runtime. You can reload, extend, freeze, override, set values, and react to changes -- all in a thread-safe manner.

---

## Reload

Reload re-reads all configuration sources, re-merges them, and updates the in-memory config. If any source fails and `ErrorPolicyRaise` is set, the reload rolls back to the previous state automatically.

=== "Full Reload"

    ```go
    err := cfg.Reload(ctx)
    if err != nil {
        log.Printf("reload failed: %v", err)
    }
    ```

=== "Incremental Reload"

    Only reload files whose mtime or SHA256 content hash has changed. This avoids unnecessary parsing when most files have not been modified.

    ```go
    err := cfg.Reload(ctx, confii.WithIncremental(true))
    ```

=== "Dry-Run Reload"

    Load and validate from sources without applying any changes. Useful for pre-flight checks in CI or before a deploy.

    ```go
    err := cfg.Reload(ctx, confii.WithDryRun(true))
    if err != nil {
        log.Printf("dry-run failed: %v", err)
    }
    // Config remains unchanged regardless of outcome
    ```

=== "Reload with Validation"

    Override the `validate_on_load` setting for this specific reload.

    ```go
    err := cfg.Reload(ctx, confii.WithReloadValidate(true))
    ```

!!! tip "Combine reload options"
    You can combine multiple reload options in a single call:

    ```go
    err := cfg.Reload(ctx,
        confii.WithIncremental(true),
        confii.WithReloadValidate(true),
    )
    ```

!!! warning "Frozen configs cannot reload"
    Calling `Reload` on a frozen config returns `ErrConfigFrozen`. Unfreeze first or use `Override` for temporary changes.

---

## Extend

Add a new loader at runtime and merge its configuration on top of the existing state. The new loader is also registered for future reloads.

```go
err := cfg.Extend(ctx, loader.NewJSON("extra.json"))
if err != nil {
    log.Fatal(err)
}

// The new source is now part of the config
val, _ := cfg.Get("extra.key")
```

!!! note "Extend vs Reload"
    `Extend` adds a **new** source and merges it immediately. `Reload` re-reads **all existing** sources. After `Extend`, the new loader is included in subsequent reloads.

---

## Override

Apply temporary scoped overrides. Returns a `restore` function that reverts to the original state. This is especially useful in tests.

```go
restore, err := cfg.Override(map[string]any{
    "database.host": "test-db",
    "database.port": 15432,
})
if err != nil {
    log.Fatal(err)
}
defer restore() // always restore when done

host, _ := cfg.Get("database.host") // "test-db"
```

!!! tip "Test-friendly pattern"
    Override temporarily unfreezes the config, applies changes, then the restore function re-freezes it back to its original state.

    ```go
    func TestDatabaseConfig(t *testing.T) {
        restore, _ := cfg.Override(map[string]any{
            "database.host": "localhost",
            "database.port": 5433,
        })
        defer restore()

        // Test code runs with overridden config
        host, _ := cfg.Get("database.host")
        assert.Equal(t, "localhost", host)
    }
    ```

---

## Freeze

Make the configuration immutable. Any mutation attempt (`Set`, `Reload`, `Extend`, `RollbackToVersion`) returns `ErrConfigFrozen`.

```go
cfg.Freeze()

err := cfg.Set("key", "value")
// err wraps ErrConfigFrozen

fmt.Println(cfg.IsFrozen()) // true
```

You can also freeze at construction time:

=== "Constructor"

    ```go
    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithFreezeOnLoad(true),
    )
    ```

=== "Builder"

    ```go
    cfg, err := confii.NewBuilder[any]().
        AddLoader(loader.NewYAML("config.yaml")).
        EnableFreezeOnLoad().
        Build(ctx)
    ```

!!! warning "ErrConfigFrozen"
    Use `errors.Is(err, confii.ErrConfigFrozen)` to check for frozen state errors:

    ```go
    if errors.Is(err, confii.ErrConfigFrozen) {
        log.Println("config is frozen, cannot modify")
    }
    ```

---

## Set

Set a value by dot-separated key path. Thread-safe and respects frozen state.

```go
err := cfg.Set("app.name", "my-service")
```

### Protected Set

Use `WithOverride(false)` to prevent overwriting an existing key. This is useful for setting defaults without clobbering user-supplied values.

```go
// Only set if "app.name" does not already exist
err := cfg.Set("app.name", "default-name", confii.WithOverride(false))
if err != nil {
    // key already exists
    log.Println(err)
}
```

!!! note "Set invalidates the typed model cache"
    After `Set`, the next call to `cfg.Typed()` will re-decode and re-validate the config.

---

## OnChange

Register callbacks that fire when configuration values change after a reload. Callbacks receive the key path, old value, and new value.

```go
cfg.OnChange(func(key string, oldVal, newVal any) {
    log.Printf("config changed: %s = %v -> %v", key, oldVal, newVal)
})

cfg.OnChange(func(key string, oldVal, newVal any) {
    if key == "log.level" {
        updateLogLevel(newVal.(string))
    }
})
```

!!! tip "Multiple callbacks"
    You can register as many callbacks as you need. They are called in registration order for each changed key. Panics in callbacks are caught and do not propagate.

!!! note "When do callbacks fire?"
    Callbacks fire during `Reload` (after changes are applied, not during dry-run). They do **not** fire on `Set` or `Override`.

---

## Full Lifecycle Example

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"

    confii "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    ctx := context.Background()

    // Create config
    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Register change callback
    cfg.OnChange(func(key string, oldVal, newVal any) {
        fmt.Printf("changed: %s\n", key)
    })

    // Extend with another source
    _ = cfg.Extend(ctx, loader.NewJSON("overrides.json"))

    // Set a value with protection
    _ = cfg.Set("feature.enabled", true, confii.WithOverride(false))

    // Temporary override for testing
    restore, _ := cfg.Override(map[string]any{"database.host": "test-db"})
    fmt.Println(cfg.GetStringOr("database.host", "")) // "test-db"
    restore()

    // Reload with dry-run first
    if err := cfg.Reload(ctx, confii.WithDryRun(true)); err != nil {
        log.Printf("dry-run failed: %v", err)
    } else {
        _ = cfg.Reload(ctx) // apply for real
    }

    // Freeze when done
    cfg.Freeze()
    if err := cfg.Set("key", "val"); errors.Is(err, confii.ErrConfigFrozen) {
        fmt.Println("config is frozen")
    }
}
```
