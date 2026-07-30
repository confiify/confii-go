# Dynamic Reloading

Confii watches configuration files on disk and automatically reloads when changes are detected, using [fsnotify](https://github.com/fsnotify/fsnotify) under the hood.

---

## Enabling File Watching

=== "Constructor"

    ```go
    cfg, err := confii.NewWithContext[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithDynamicReloading(true),
    )
    ```

=== "Builder"

    ```go
    cfg, err := confii.NewBuilder[any]().
        AddLoader(loader.NewYAML("config.yaml")).
        EnableDynamicReloading().
        BuildWithContext(ctx)
    ```

=== "Self-Config"

    ```yaml
    # .confii.yaml
    dynamic_reloading: true
    sources:
      - type: yaml
        path: config.yaml
    ```

Once enabled, Confii starts a background goroutine that watches the **directories** containing your config files for changes.

---

## How Change Detection Works

The file watcher uses fsnotify to monitor directories (not individual files, which avoids issues with editors that perform atomic saves via rename).

1. **fsnotify** reports a `Write` or `Create` event on a watched directory
2. Confii checks if the event file matches one of its tracked source files (by absolute path)
3. If it matches, `Reload` is triggered automatically
4. The reload uses incremental detection (mtime + SHA256 hash) to skip files that have not actually changed

```text
File edit detected
    |
    v
fsnotify event (Write/Create)
    |
    v
Is file in watched set? --No--> Ignore
    |
   Yes
    |
    v
cfg.ReloadWithContext(ctx)
    |
    v
Incremental check (mtime + SHA256)
    |
    v
Re-merge and notify callbacks
```

!!! note "Events that trigger reload"
    Only `Write` and `Create` events trigger a reload. Rename, chmod, and remove events are ignored.

---

## OnChange Callback Integration

Combine file watching with change callbacks to react to configuration changes in real-time:

```go
cfg, _ := confii.NewWithContext[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithDynamicReloading(true),
)

cfg.OnChange(func(key string, oldVal, newVal any) {
    log.Printf("config changed: %s = %v -> %v", key, oldVal, newVal)

    switch key {
    case "log.level":
        updateLogLevel(newVal.(string))
    case "feature_flags.new_ui":
        toggleFeature("new_ui", newVal.(bool))
    }
})
```

!!! tip "Callback safety"
    Panics in change callbacks are caught and logged. A panic in one callback does not prevent other callbacks from running.

---

## StopWatching

Always stop the watcher when your application is shutting down to release file descriptors and stop the background goroutine:

```go
defer cfg.StopWatching()
```

Or in a graceful shutdown handler:

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

go func() {
    <-sigCh
    cfg.StopWatching()
    os.Exit(0)
}()
```

!!! warning "Always stop watching"
    Forgetting to call `StopWatching` can lead to goroutine leaks and open file descriptors. Use `defer` or a shutdown hook.

---

## Incremental Reload

When reload is triggered (either by the file watcher or manually), Confii fingerprints each local file with metadata and SHA-256 content hashing:

1. **mtime metadata** -- records when the filesystem reports a source update
2. **SHA-256 hash** -- decides whether the file content actually changed

This means:

- If you `touch` or copy an identical file, the hash suppresses a false reload.
- If content changes without an observable mtime tick, the hash still detects it.
- Only changed file loaders run; unchanged composed layers are reused and re-merged in their original precedence order.
- HTTP, cloud, environment, and other sources without a local fingerprint are refreshed on every incremental call so remote changes do not become permanently stale.

```go
// Manual incremental reload
err := cfg.ReloadWithContext(ctx, confii.WithIncremental(true))
```

The file watcher calls `Reload(ctx)`, whose default is the incremental behavior above. Pass `WithIncremental(false)` to force every loader to run.

---

## Best Practices for Production

!!! tip "Use with validation"
    Enable `WithValidateOnLoad(true)` so that invalid config changes are automatically rejected and rolled back:

    ```go
    cfg, _ := confii.NewWithContext[AppConfig](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithDynamicReloading(true),
        confii.WithValidateOnLoad(true),
    )
    ```

    If the new config fails validation, the reload is rolled back to the previous state.

!!! tip "Combine with observability"
    Enable metrics and events to monitor reloads in production:

    ```go
    cfg.EnableObservability()
    emitter := cfg.EnableEvents()

    emitter.On("reload", func(args ...any) {
        metrics.IncrCounter("config.reloads", 1)
    })
    ```

!!! warning "Do not watch files on networked or ephemeral filesystems"
    fsnotify relies on OS-level filesystem events (inotify on Linux, kqueue on macOS). Network filesystems (NFS, CIFS) and container volumes may not reliably produce these events. For such environments, use a manual polling approach with `cfg.ReloadWithContext(ctx)` on a timer instead.

!!! tip "Rate limiting"
    Editors may produce multiple write events in quick succession (e.g., write temp file, rename). fsnotify may fire multiple events for a single save. Confii's incremental check (mtime + hash) mitigates redundant reloads at the source level.

---

## Full Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"

    confii "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
)

func main() {
    ctx := context.Background()

    cfg, err := confii.NewWithContext[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithDynamicReloading(true),
        confii.WithValidateOnLoad(true),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer cfg.StopWatching()

    cfg.OnChange(func(key string, oldVal, newVal any) {
        fmt.Printf("[config] %s changed: %v -> %v\n", key, oldVal, newVal)
    })

    fmt.Println("Watching for config changes. Press Ctrl+C to exit.")

    // Block until signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh

    fmt.Println("Shutting down.")
}
```
