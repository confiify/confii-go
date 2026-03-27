# Diff & Drift Detection

Confii can compare two configurations and produce a structured diff, or detect unintended changes (drift) against a known baseline.

---

## Diffing Two Configs

The `Diff` method compares two `Config` instances and returns a list of `ConfigDiff` entries:

```go
diffs := cfg1.Diff(cfg2)

for _, d := range diffs {
    switch d.Type {
    case diff.Added:
        fmt.Printf("+ %s = %v\n", d.Path, d.NewValue)
    case diff.Removed:
        fmt.Printf("- %s = %v\n", d.Path, d.OldValue)
    case diff.Modified:
        fmt.Printf("~ %s: %v -> %v\n", d.Path, d.OldValue, d.NewValue)
    }
}
```

You can also diff raw maps directly using the `diff` package:

```go
import "github.com/confiify/confii-go/diff"

diffs := diff.Diff(map1, map2)
```

---

## ConfigDiff Type

Each difference is represented as a `ConfigDiff` struct:

```go
type ConfigDiff struct {
    Key         string       `json:"key"`
    Type        DiffType     `json:"type"`
    OldValue    any          `json:"old_value,omitempty"`
    NewValue    any          `json:"new_value,omitempty"`
    Path        string       `json:"path"`
    NestedDiffs []ConfigDiff `json:"nested_diffs,omitempty"`
}
```

| Field | Description |
|-------|-------------|
| `Key` | The immediate key name |
| `Type` | One of `Added`, `Removed`, `Modified` |
| `OldValue` | Value in the first config (nil for `Added`) |
| `NewValue` | Value in the second config (nil for `Removed`) |
| `Path` | Full dot-separated path (e.g., `database.host`) |
| `NestedDiffs` | Recursive diffs for nested map values |

---

## DiffType

```go
const (
    Added    DiffType = "added"
    Removed  DiffType = "removed"
    Modified DiffType = "modified"
)
```

- **Added** -- key exists in the second config but not the first
- **Removed** -- key exists in the first config but not the second
- **Modified** -- key exists in both but with different values

!!! note "Nested diffs"
    When both values are maps, Confii diffs them recursively. The parent entry has `Type: Modified` with the detail in `NestedDiffs`.

---

## Summary

Get a count summary of differences:

```go
summary := diff.Summary(diffs)
fmt.Printf("Total: %d (added: %d, removed: %d, modified: %d)\n",
    summary["total"],
    summary["added"],
    summary["removed"],
    summary["modified"],
)
```

The summary recursively counts nested diffs as well.

---

## ToJSON

Serialize diffs to JSON for reporting, logging, or pipeline artifacts:

```go
jsonStr, err := diff.ToJSON(diffs)
if err != nil {
    log.Fatal(err)
}
fmt.Println(jsonStr)
```

**Example output:**

```json
[
  {
    "key": "host",
    "type": "modified",
    "old_value": "localhost",
    "new_value": "prod-db.example.com",
    "path": "database.host"
  },
  {
    "key": "cache_ttl",
    "type": "added",
    "new_value": 300,
    "path": "cache_ttl"
  }
]
```

---

## Drift Detection

A `DriftDetector` compares the actual running configuration against an intended baseline to detect unintended changes.

### Using Config.DetectDrift

```go
intended := map[string]any{
    "database": map[string]any{
        "host": "prod-db.example.com",
        "port": 5432,
    },
    "cache": map[string]any{
        "enabled": true,
    },
}

drifts := cfg.DetectDrift(intended)
if len(drifts) > 0 {
    fmt.Printf("Drift detected! %d differences\n", len(drifts))
    for _, d := range drifts {
        fmt.Printf("  %s: %s\n", d.Path, d.Type)
    }
}
```

### Using DriftDetector Directly

For repeated checks against the same baseline, create a `DriftDetector`:

```go
import "github.com/confiify/confii-go/diff"

detector := diff.NewDriftDetector(intendedConfig)

// Check if any drift exists
if detector.HasDrift(cfg.ToDict()) {
    fmt.Println("Configuration has drifted from baseline!")
}

// Get detailed drift information
drifts := detector.DetectDrift(cfg.ToDict())
for _, d := range drifts {
    fmt.Printf("  %s (%s): %v -> %v\n", d.Path, d.Type, d.OldValue, d.NewValue)
}
```

---

## Use Cases

### CI Pipeline Drift Checks

Run drift detection in CI to ensure deployed configs match the intended state:

```go
func TestConfigDrift(t *testing.T) {
    // Load the intended baseline (e.g., checked into git)
    baseline, _ := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config/baseline.yaml")),
        confii.WithEnv("production"),
    )

    // Load the actual deployed config
    actual, _ := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config/deployed.yaml")),
        confii.WithEnv("production"),
    )

    drifts := baseline.Diff(actual)
    if len(drifts) > 0 {
        jsonStr, _ := diff.ToJSON(drifts)
        t.Fatalf("config drift detected:\n%s", jsonStr)
    }
}
```

### Config Auditing

Compare configs across environments to document differences:

```go
devCfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("development"),
)

prodCfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithEnv("production"),
)

diffs := devCfg.Diff(prodCfg)
summary := diff.Summary(diffs)
fmt.Printf("Dev vs Prod: %d differences\n", summary["total"])

// Export for review
jsonStr, _ := diff.ToJSON(diffs)
os.WriteFile("env-diff.json", []byte(jsonStr), 0644)
```

### Reload Change Report

Diff before and after a reload to know exactly what changed:

```go
before := copyConfig(cfg.ToDict())
cfg.Reload(ctx)
after := cfg.ToDict()

changes := diff.Diff(before, after)
if len(changes) > 0 {
    fmt.Printf("Reload changed %d keys\n", diff.Summary(changes)["total"])
}
```

!!! tip "CLI diff"
    The CLI tool provides a `diff` command for comparing configs from the command line:

    ```bash
    confii diff dev production --loader1 yaml:config.yaml --loader2 yaml:config.yaml
    ```
