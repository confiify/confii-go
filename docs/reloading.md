# Dynamic Reloading

Confii watches configuration files on disk and automatically reloads when changes are detected, using [fsnotify](https://github.com/fsnotify/fsnotify) under the hood.

---

## Enabling File Watching

=== "Constructor"

    ```go
    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithDynamicReloading(true),
    )
    ```

=== "Builder"

    ```go
    cfg, err := confii.NewBuilder[any]().
        AddLoader(loader.NewYAML("config.yaml")).
        EnableDynamicReloading().
        Build(ctx)
    ```

=== "Self-Config"

    ```yaml
    # .confii.yaml
    dynamic_reloading: true
    default_files:
      - config.yaml
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
cfg.Reload(ctx)
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
cfg, _ := confii.New[any](ctx,
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

When reload is triggered (either by the file watcher or manually), Confii uses a two-step change detection to avoid unnecessary work:

1. **mtime check** -- compare the file's modification time against the last known value
2. **SHA256 hash** -- if mtime changed, compute and compare the file's content hash

This means:

- If you `touch` a file without changing its content, the mtime changes but the hash stays the same -- the file is still considered "changed" (mtime is checked first for speed)
- If the OS updates mtime due to a copy or move, the hash comparison catches false positives

```go
// Manual incremental reload
err := cfg.Reload(ctx, confii.WithIncremental(true))
```

The file watcher triggers a full `Reload(ctx)` which defaults to incremental behavior (the `reloadOpts` default `incremental` is `true`).

---

## Best Practices for Production

!!! tip "Use with validation"
    Enable `WithValidateOnLoad(true)` so that invalid config changes are automatically rejected and rolled back:

    ```go
    cfg, _ := confii.New[AppConfig](ctx,
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
    fsnotify relies on OS-level filesystem events (inotify on Linux, kqueue on macOS). Network filesystems (NFS, CIFS) and container volumes may not reliably produce these events. For such environments, use a manual polling approach with `cfg.Reload(ctx)` on a timer instead.

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

    confii "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    ctx := context.Background()

    cfg, err := confii.New[any](ctx,
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
