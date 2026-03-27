# Versioning & Rollback

Confii can snapshot configuration state, compare versions over time, and rollback to a previous state. Versions are persisted to disk as JSON files.

---

## Enabling Versioning

Call `EnableVersioning` with a storage path and maximum number of versions to keep:

```go
vm := cfg.EnableVersioning("/tmp/config-versions", 100)
```

| Parameter | Description | Default |
|-----------|-------------|---------|
| `storagePath` | Directory for storing version JSON files | `.confii/versions` |
| `maxVersions` | Maximum number of versions to retain | `100` |

!!! note "Default storage path"
    If you pass an empty string for `storagePath`, versions are stored in `.confii/versions` relative to the working directory.

---

## Saving a Version

`SaveVersion` captures an immutable snapshot of the current configuration state. You can attach arbitrary metadata (author, environment, deployment ID, etc.):

```go
v1, err := cfg.SaveVersion(map[string]any{
    "author":    "deploy-bot",
    "env":       "production",
    "deploy_id": "deploy-2024-03-15",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Saved version: %s at %s\n", v1.VersionID, v1.DateTime)
```

Each version gets a unique ID derived from a SHA256 hash of the config content and timestamp.

The `Version` struct:

```go
type Version struct {
    VersionID string         `json:"version_id"`
    Config    map[string]any `json:"config"`
    Timestamp float64        `json:"timestamp"`
    DateTime  string         `json:"datetime"`
    Metadata  map[string]any `json:"metadata,omitempty"`
}
```

!!! tip "Metadata is optional"
    Pass `nil` if you do not need metadata:

    ```go
    v, _ := cfg.SaveVersion(nil)
    ```

---

## Listing Versions

`ListVersions` returns all stored versions sorted by timestamp, newest first:

```go
vm := cfg.EnableVersioning("/tmp/config-versions", 100)
versions := vm.ListVersions()

for _, v := range versions {
    fmt.Printf("  %s  %s  %v\n", v.VersionID, v.DateTime, v.Metadata)
}
```

!!! note "Disk scanning"
    `ListVersions` scans the storage directory for any version files that have not been loaded into memory yet. This means versions saved by previous runs of your application are also listed.

---

## Getting a Specific Version

```go
v := vm.GetVersion("a1b2c3d4e5f6g7h8")
if v == nil {
    log.Fatal("version not found")
}
fmt.Printf("Config at %s had %d keys\n", v.DateTime, len(v.Config))
```

`GetVersion` first checks the in-memory cache, then falls back to reading from disk.

---

## Latest Version

```go
latest := vm.LatestVersion()
if latest != nil {
    fmt.Printf("Latest version: %s (%s)\n", latest.VersionID, latest.DateTime)
}
```

---

## Diffing Versions

Compare two version snapshots to see what changed between them:

```go
diffs, err := vm.DiffVersions(v1.VersionID, v2.VersionID)
if err != nil {
    log.Fatal(err)
}

for _, d := range diffs {
    fmt.Printf("  %s: %s (%v -> %v)\n",
        d["path"], d["type"], d["old_value"], d["new_value"])
}
```

Each diff entry is a map with keys: `path`, `type` (`added`, `removed`, `modified`), `old_value`, `new_value`.

---

## Rolling Back

Restore the configuration to a previous version snapshot:

```go
err := cfg.RollbackToVersion(v1.VersionID)
if err != nil {
    log.Fatal(err)
}

// Config is now restored to v1's state
host, _ := cfg.Get("database.host")
fmt.Println(host) // value from v1
```

!!! warning "Rollback replaces the entire config"
    Rollback replaces both `envConfig` and `mergedConfig` with the snapshot data. The typed model cache is invalidated. Subsequent `Typed()` calls will re-decode.

!!! warning "Frozen configs cannot rollback"
    `RollbackToVersion` returns `ErrConfigFrozen` if the config is frozen.

---

## Storage: Disk-Based JSON Files

Versions are persisted as individual JSON files in the storage directory:

```text
/tmp/config-versions/
  a1b2c3d4.json
  e5f6g7h8.json
  i9j0k1l2.json
```

Each file contains the full `Version` struct serialized as indented JSON:

```json
{
  "version_id": "a1b2c3d4",
  "config": {
    "database": {
      "host": "prod-db.example.com",
      "port": 5432
    }
  },
  "timestamp": 1710500000,
  "datetime": "2024-03-15T12:00:00Z",
  "metadata": {
    "author": "deploy-bot"
  }
}
```

---

## Eviction of Old Versions

When the number of stored versions exceeds `maxVersions`, Confii automatically evicts the oldest versions:

- Versions are sorted by timestamp
- The oldest versions beyond the limit are deleted from both memory and disk
- Eviction runs automatically after each `SaveVersion` call

```go
// Keep only the last 10 versions
vm := cfg.EnableVersioning("/tmp/config-versions", 10)

// After saving the 11th version, the oldest is automatically deleted
```

---

## Full Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    confii "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    ctx := context.Background()

    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Enable versioning
    vm := cfg.EnableVersioning("/tmp/config-versions", 50)

    // Save initial version
    v1, _ := cfg.SaveVersion(map[string]any{
        "author": "deploy-bot",
        "reason": "initial deploy",
    })
    fmt.Printf("Saved v1: %s\n", v1.VersionID)

    // Make some changes
    cfg.Set("database.pool_size", 20)
    cfg.Set("cache.ttl", 600)

    // Save after changes
    v2, _ := cfg.SaveVersion(map[string]any{
        "author": "deploy-bot",
        "reason": "increase pool size and cache TTL",
    })
    fmt.Printf("Saved v2: %s\n", v2.VersionID)

    // Compare versions
    diffs, _ := vm.DiffVersions(v1.VersionID, v2.VersionID)
    fmt.Printf("Changes between v1 and v2: %d\n", len(diffs))
    for _, d := range diffs {
        fmt.Printf("  %s: %s\n", d["path"], d["type"])
    }

    // List all versions
    fmt.Println("\nAll versions:")
    for _, v := range vm.ListVersions() {
        fmt.Printf("  %s  %s  %v\n", v.VersionID, v.DateTime, v.Metadata)
    }

    // Rollback to v1
    err = cfg.RollbackToVersion(v1.VersionID)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("\nRolled back to v1")

    // Verify rollback
    poolSize, _ := cfg.Get("database.pool_size")
    fmt.Printf("database.pool_size: %v\n", poolSize)
}
```
