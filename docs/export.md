# Export & Documentation

Confii can export configuration data to JSON, YAML, and TOML formats, and generate documentation from the current config state.

---

## Export

### Export to Bytes

The `Export` method serializes the effective configuration to the specified format and returns the raw bytes:

=== "JSON"

    ```go
    data, err := cfg.Export("json")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(string(data))
    ```

    Output:

    ```json
    {
      "database": {
        "host": "prod-db.example.com",
        "port": 5432
      },
      "app": {
        "name": "my-service"
      }
    }
    ```

=== "YAML"

    ```go
    data, err := cfg.Export("yaml")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(string(data))
    ```

    Output:

    ```yaml
    app:
      name: my-service
    database:
      host: prod-db.example.com
      port: 5432
    ```

=== "TOML"

    ```go
    data, err := cfg.Export("toml")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(string(data))
    ```

    Output:

    ```toml
    [app]
      name = "my-service"

    [database]
      host = "prod-db.example.com"
      port = 5432
    ```

### Export to File

Pass an output path as the second argument to write directly to a file:

```go
data, err := cfg.Export("json", "/path/to/output.json")
if err != nil {
    log.Fatal(err)
}
// data also contains the bytes, and the file is written
```

```go
// Export as YAML to file
_, err := cfg.Export("yaml", "config-snapshot.yaml")

// Export as TOML to file
_, err := cfg.Export("toml", "config-snapshot.toml")
```

!!! note "File permissions"
    Exported files are written with `0644` permissions.

---

## Documentation Generation

### GenerateDocs

Generate a reference document from the current configuration state. Each key is listed with its type, current value, and source.

=== "Markdown"

    ```go
    docs, err := cfg.GenerateDocs("markdown")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print(docs)
    ```

    Output:

    ```markdown
    | Key | Type | Value | Source |
    |-----|------|-------|--------|
    | `app.name` | string | `my-service` | config.yaml |
    | `database.host` | string | `prod-db.example.com` | prod.yaml |
    | `database.port` | int | `5432` | base.yaml |
    ```

=== "JSON"

    ```go
    docs, err := cfg.GenerateDocs("json")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print(docs)
    ```

    Output:

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
        "current_value": "prod-db.example.com",
        "source": "prod.yaml"
      },
      {
        "key": "database.port",
        "type": "int",
        "current_value": 5432,
        "source": "base.yaml"
      }
    ]
    ```

!!! tip "Source tracking in docs"
    The `source` field in generated docs is populated from Confii's source tracker. Enable `WithDebugMode(true)` for the most accurate source attribution.

---

## Use Cases

### Config Snapshots

Save the current state of your configuration for auditing or debugging:

```go
func snapshotConfig(cfg *confii.Config[any], env string) error {
    filename := fmt.Sprintf("config-snapshot-%s-%s.json",
        env, time.Now().Format("2006-01-02T15-04-05"))

    _, err := cfg.Export("json", filename)
    return err
}
```

### Documentation Generation in CI

Automatically generate config docs as part of your build pipeline:

```go
func generateConfigDocs(cfg *confii.Config[any]) error {
    // Markdown for human-readable docs
    md, err := cfg.GenerateDocs("markdown")
    if err != nil {
        return err
    }
    if err := os.WriteFile("docs/config-reference.md", []byte(md), 0644); err != nil {
        return err
    }

    // JSON for machine-readable docs
    jsonDocs, err := cfg.GenerateDocs("json")
    if err != nil {
        return err
    }
    return os.WriteFile("docs/config-reference.json", []byte(jsonDocs), 0644)
}
```

### Format Conversion

Convert between config formats:

```go
// Load YAML, export as TOML
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("production"),
)

_, err := cfg.Export("toml", "config.toml")
if err != nil {
    log.Fatal(err)
}
```

### CLI Export

Use the CLI tool to export from the command line:

```bash
# Export as JSON to stdout
confii export production -l yaml:config.yaml -f json

# Export as YAML to file
confii export production -l yaml:config.yaml -f yaml -o config-export.yaml

# Generate markdown docs
confii docs production -l yaml:config.yaml -f markdown -o CONFIG.md
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
        ),
        confii.WithEnv("production"),
        confii.WithDebugMode(true),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Export to all formats
    jsonData, _ := cfg.Export("json")
    fmt.Printf("JSON (%d bytes)\n", len(jsonData))

    yamlData, _ := cfg.Export("yaml")
    fmt.Printf("YAML (%d bytes)\n", len(yamlData))

    tomlData, _ := cfg.Export("toml")
    fmt.Printf("TOML (%d bytes)\n", len(tomlData))

    // Export to file
    _, _ = cfg.Export("json", "snapshot.json")
    fmt.Println("Snapshot saved to snapshot.json")

    // Generate docs
    md, _ := cfg.GenerateDocs("markdown")
    fmt.Println("\nConfiguration Reference:")
    fmt.Print(md)
}
```
