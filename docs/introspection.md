# Introspection & Source Tracking

Confii tracks the origin of every configuration value -- which file it came from, which loader loaded it, how many times it was overridden, and the full override history. This makes debugging configuration issues straightforward.

---

## Explain

`Explain` returns detailed resolution information for a key as a map. It tells you the current value, its source, the loader type, the active environment, and the override count.

```go
info := cfg.Explain("database.host")
fmt.Printf("%+v\n", info)
```

**Example output:**

```go
map[string]any{
    "exists":         true,
    "key":            "database.host",
    "value":          "prod-db.example.com",
    "current_value":  "prod-db.example.com",
    "source":         "config.yaml",
    "loader_type":    "YAMLLoader",
    "environment":    "production",
    "override_count": 2,
    "override_history": []map[string]any{
        {"value": "localhost", "source": "base.yaml", "loader_type": "YAMLLoader"},
        {"value": "staging-db", "source": "staging.yaml", "loader_type": "YAMLLoader"},
    },
}
```

!!! note "Override history requires debug mode"
    The `override_history` field is only populated when `WithDebugMode(true)` is enabled. Without it, you still get the override count but not the history.

When the key does not exist, `Explain` returns the available keys to help with typos:

```go
info := cfg.Explain("databse.host") // typo
// info["exists"] == false
// info["available_keys"] == ["database.host", "database.port", ...]
```

---

## Schema

`Schema` returns type information for a key -- its Go type, current value, and whether it exists.

```go
schema := cfg.Schema("database.port")
fmt.Println(schema)
// map[key:database.port exists:true value:5432 type:int]
```

---

## Layers

`Layers` returns the source stack showing each loader, its type, and the keys it contributed. This visualizes the order of precedence.

```go
layers := cfg.Layers()
for _, layer := range layers {
    fmt.Printf("Source: %s (%s) - %d keys\n",
        layer["source"],
        layer["loader_type"],
        layer["key_count"],
    )
}
```

**Example output:**

```text
Source: base.yaml (YAMLLoader) - 12 keys
Source: prod.yaml (YAMLLoader) - 5 keys
Source: APP (EnvironmentLoader) - 3 keys
```

---

## Debug Mode

Enable `WithDebugMode(true)` to track the full override history for every key. Without it, Confii tracks sources and override counts but not the history chain.

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("base.yaml"),
        loader.NewYAML("prod.yaml"),
    ),
    confii.WithDebugMode(true),
)
```

!!! warning "Performance"
    Debug mode stores additional data for every override. In production with many sources and keys, this increases memory usage. Enable it only when needed, or use it in staging/development environments.

---

## Source Tracking Methods

### GetSourceInfo

Returns the full `SourceInfo` struct for a key:

```go
info := cfg.GetSourceInfo("database.host")
if info != nil {
    fmt.Printf("Key:       %s\n", info.Key)
    fmt.Printf("Value:     %v\n", info.Value)
    fmt.Printf("Source:    %s\n", info.SourceFile)
    fmt.Printf("Loader:    %s\n", info.LoaderType)
    fmt.Printf("Overrides: %d\n", info.OverrideCount)
    fmt.Printf("Timestamp: %s\n", info.Timestamp)
}
```

The `SourceInfo` struct:

```go
type SourceInfo struct {
    Key           string          `json:"key"`
    Value         any             `json:"value"`
    SourceFile    string          `json:"source_file"`
    LoaderType    string          `json:"loader_type"`
    LineNumber    int             `json:"line_number,omitempty"`
    Environment   string          `json:"environment,omitempty"`
    OverrideCount int             `json:"override_count"`
    History       []OverrideEntry `json:"history,omitempty"`
    Timestamp     time.Time       `json:"timestamp"`
}
```

### GetOverrideHistory

Returns the override history chain for a key. Requires `WithDebugMode(true)`.

```go
history := cfg.GetOverrideHistory("database.host")
for i, entry := range history {
    fmt.Printf("%d. value=%v from=%s via=%s\n",
        i+1, entry.Value, entry.Source, entry.LoaderType)
}
```

### GetConflicts

Returns all keys that have been overridden at least once:

```go
conflicts := cfg.GetConflicts()
for key, info := range conflicts {
    fmt.Printf("%s was overridden %d time(s), final source: %s\n",
        key, info.OverrideCount, info.SourceFile)
}
```

!!! tip "Auditing overrides"
    Use `GetConflicts` in CI to detect unexpected overrides. If a key you expect to come from `base.yaml` is being overridden by another source, this tells you immediately.

### GetSourceStatistics

Returns aggregated statistics about configuration sources:

```go
stats := cfg.GetSourceStatistics()
fmt.Println(stats)
// map[
//   total_keys:15
//   sources:map[base.yaml:10 prod.yaml:5]
//   loader_types:map[YAMLLoader:15]
//   total_overrides:3
// ]
```

### FindKeysFromSource

Returns all keys that originated from sources matching a substring pattern:

```go
keys := cfg.FindKeysFromSource("prod.yaml")
fmt.Println(keys)
// [database.host cache.ttl logging.level]
```

---

## Debug Reports

### PrintDebugInfo

Returns a human-readable debug report for a specific key, or all keys if the argument is empty:

```go
// Single key
fmt.Print(cfg.PrintDebugInfo("database.host"))
```

**Output:**

```text
Key:       database.host
Value:     prod-db.example.com
Source:    prod.yaml
Loader:    YAMLLoader
Overrides: 1
History:
  1. localhost (from base.yaml via YAMLLoader)
```

```go
// All keys
fmt.Print(cfg.PrintDebugInfo(""))
```

### ExportDebugReport

Exports the full source tracking data as a JSON file:

```go
err := cfg.ExportDebugReport("debug-report.json")
```

The output is a JSON object keyed by config path, with each value being the full `SourceInfo` struct.

---

## Documentation Generation

### GenerateDocs

Generate a reference document from the current config state:

=== "Markdown"

    ```go
    docs, err := cfg.GenerateDocs("markdown")
    fmt.Print(docs)
    ```

    **Output:**

    ```markdown
    | Key | Type | Value | Source |
    |-----|------|-------|--------|
    | `app.name` | string | `my-service` | config.yaml |
    | `database.host` | string | `prod-db` | prod.yaml |
    | `database.port` | int | `5432` | base.yaml |
    ```

=== "JSON"

    ```go
    docs, err := cfg.GenerateDocs("json")
    fmt.Print(docs)
    ```

    **Output:**

    ```json
    [
      {
        "key": "app.name",
        "type": "string",
        "current_value": "my-service",
        "source": "config.yaml"
      },
      {
        "key": "database.host",
        "type": "string",
        "current_value": "prod-db",
        "source": "prod.yaml"
      }
    ]
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
        confii.WithLoaders(
            loader.NewYAML("base.yaml"),
            loader.NewYAML("prod.yaml"),
            loader.NewEnvironment("APP"),
        ),
        confii.WithEnv("production"),
        confii.WithDebugMode(true),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Explain a key
    info := cfg.Explain("database.host")
    fmt.Printf("database.host comes from: %s (overridden %v times)\n",
        info["source"], info["override_count"])

    // Show layers
    for _, layer := range cfg.Layers() {
        fmt.Printf("  %s (%s): %d keys\n",
            layer["source"], layer["loader_type"], layer["key_count"])
    }

    // Find conflicts
    for key, si := range cfg.GetConflicts() {
        fmt.Printf("  conflict: %s overridden %d time(s)\n", key, si.OverrideCount)
    }

    // Export full report
    _ = cfg.ExportDebugReport("debug-report.json")
    fmt.Println("Debug report saved to debug-report.json")
}
```
