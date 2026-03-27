# Hook System

Hooks transform configuration values at access time. Instead of modifying stored data, hooks act as a middleware layer -- every time you call `Get`, `GetString`, or any getter method, registered hooks process the value before it reaches your code.

---

## How Hooks Work

When you access a leaf value via any getter method, Confii's `hook.Processor` runs all applicable hooks in a defined order. Each hook receives the key path and current value, and returns a (potentially transformed) value. The output of one hook becomes the input to the next.

```text
cfg.Get("database.password")
    |
    v
Raw value: "${secret:db/password}"
    |
    v  [key hooks for "database.password"]
    v  [value hooks matching "${secret:db/password}"]
    v  [condition hooks where condition returns true]
    v  [global hooks]
    |
    v
Final value: "s3cret-passw0rd"
```

!!! note "Hooks only apply to leaf values"
    When `Get` returns a map (e.g., `cfg.Get("database")`), hooks are **not** applied. This prevents unintended transformations on intermediate map nodes.

---

## The 4 Hook Types

### Key Hook

Fires when the key **exactly matches** a registered path. Use this for targeted transformations on specific config values.

```go
hp := cfg.HookProcessor()

hp.RegisterKeyHook("app.name", func(key string, value any) any {
    return strings.ToUpper(value.(string))
})

// cfg.Get("app.name") → "MY-SERVICE" (uppercased)
// cfg.Get("app.port") → 8080 (unaffected)
```

### Value Hook

Fires when the value **exactly matches** a registered value. Only works for comparable (hashable) values -- strings, numbers, booleans.

```go
hp.RegisterValueHook("PLACEHOLDER", func(key string, value any) any {
    return "actual-value"
})

// Any key whose value is exactly "PLACEHOLDER" → "actual-value"
```

!!! note "Value hooks match the raw value"
    The comparison happens on the **current** value at that point in the hook chain. If a key hook already transformed the value, the value hook compares against the transformed result.

### Condition Hook

Fires when a custom condition function returns `true`. This is the most flexible type -- you can match patterns, check types, or apply any logic.

```go
hp.RegisterConditionHook(
    // Condition: fire for any key containing "password"
    func(key string, value any) bool {
        return strings.Contains(key, "password")
    },
    // Hook: mask the value
    func(key string, value any) any {
        return "********"
    },
)

// cfg.Get("database.password") → "********"
// cfg.Get("smtp.password")     → "********"
// cfg.Get("database.host")     → "prod-db.example.com" (unaffected)
```

### Global Hook

Fires for **every** value access. Use sparingly, as this runs on every `Get` call.

```go
hp.RegisterGlobalHook(func(key string, value any) any {
    // Log every access
    log.Printf("config access: %s = %v", key, value)
    return value
})
```

---

## Evaluation Order

Hooks are evaluated in a strict order. Within each category, hooks fire in registration order.

```text
1. Key hooks       (exact key match)
2. Value hooks     (exact value match)
3. Condition hooks (custom predicate)
4. Global hooks    (every access)
```

Each hook's output becomes the next hook's input. This means:

- A key hook can transform a value before a condition hook sees it.
- A global hook always sees the final result of all previous hooks.

```go
hp.RegisterKeyHook("app.name", func(key string, value any) any {
    return strings.TrimSpace(value.(string))  // Step 1: trim whitespace
})

hp.RegisterGlobalHook(func(key string, value any) any {
    if s, ok := value.(string); ok {
        return strings.ToUpper(s)  // Step 2: uppercase all strings
    }
    return value
})

// cfg.Get("app.name") with value "  my-service  "
// After key hook: "my-service"
// After global hook: "MY-SERVICE"
```

---

## Built-in Hooks

### EnvExpander

Replaces `${VAR}` placeholders in string values with OS environment variable values. Enabled by default via `WithEnvExpander(true)`.

```go
// Enabled by default -- to disable:
confii.WithEnvExpander(false)
```

```yaml title="config.yaml"
database:
  host: ${DB_HOST}
  password: ${DB_PASSWORD}
  url: postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:5432/mydb
```

```bash
export DB_HOST=prod-db.example.com
export DB_USER=admin
export DB_PASSWORD=s3cret
```

```go
host, _ := cfg.Get("database.host")
// "prod-db.example.com"

url, _ := cfg.Get("database.url")
// "postgres://admin:s3cret@prod-db.example.com:5432/mydb"
```

!!! tip "Unknown variables are left unchanged"
    If an environment variable is not set, the `${VAR}` placeholder is left as-is in the value. This makes it safe to use placeholders that are resolved by other hooks (like the secret resolver).

The pattern matched is `${VAR_NAME}` where `VAR_NAME` can contain letters, numbers, and underscores. It does **not** match `${secret:...}` patterns (which use a colon after the opening), so environment expansion and secret resolution coexist safely.

### TypeCast

Automatically converts string values to their most appropriate Go type (`bool`, `int`, `float64`). Enabled by default via `WithTypeCasting(true)`.

```go
// Enabled by default -- to disable:
confii.WithTypeCasting(false)
```

This is especially useful when values come from environment variables, which are always strings:

```bash
export APP_PORT=8080
export APP_DEBUG=true
export APP_THRESHOLD=0.95
```

```go
// Without TypeCast: all values are strings
port, _ := cfg.Get("app.port")     // "8080" (string)

// With TypeCast (default): values are converted
port, _ := cfg.Get("app.port")     // 8080 (int)
debug, _ := cfg.Get("app.debug")   // true (bool)
threshold, _ := cfg.Get("app.threshold") // 0.95 (float64)
```

---

## Custom Hook Examples

### Masking Sensitive Values

```go
sensitiveKeys := map[string]bool{
    "database.password": true,
    "api.secret_key":    true,
    "smtp.password":     true,
}

hp.RegisterConditionHook(
    func(key string, value any) bool {
        return sensitiveKeys[key]
    },
    func(key string, value any) any {
        if s, ok := value.(string); ok && len(s) > 0 {
            return s[:1] + strings.Repeat("*", len(s)-1)
        }
        return "****"
    },
)

// cfg.Get("database.password") → "s*****" (first char + asterisks)
```

### URL Construction

```go
hp.RegisterKeyHook("database.url", func(key string, value any) any {
    host := cfg.GetStringOr("database.host", "localhost")
    port := cfg.GetIntOr("database.port", 5432)
    name := cfg.GetStringOr("database.name", "mydb")
    return fmt.Sprintf("postgres://%s:%d/%s", host, port, name)
})
```

### Default Value Injection

```go
defaults := map[string]any{
    "server.timeout":  30,
    "server.max_body": "10MB",
    "cache.ttl":       300,
}

hp.RegisterConditionHook(
    func(key string, value any) bool {
        return value == nil || value == ""
    },
    func(key string, value any) any {
        if d, ok := defaults[key]; ok {
            return d
        }
        return value
    },
)
```

### Prefix Stripping

```go
hp.RegisterConditionHook(
    func(key string, value any) bool {
        s, ok := value.(string)
        return ok && strings.HasPrefix(s, "base64:")
    },
    func(key string, value any) any {
        s := value.(string)
        decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "base64:"))
        if err != nil {
            return value
        }
        return string(decoded)
    },
)

// Config value: "base64:aGVsbG8gd29ybGQ="
// After hook:   "hello world"
```

---

## HookProcessor API

The `hook.Processor` is accessed via `cfg.HookProcessor()`:

```go
hp := cfg.HookProcessor()
```

### Registration Methods

| Method | Signature | When It Fires |
|---|---|---|
| `RegisterKeyHook` | `(key string, h Func)` | Key exactly matches `key` |
| `RegisterValueHook` | `(value any, h Func)` | Value exactly equals `value` |
| `RegisterConditionHook` | `(cond Condition, h Func)` | `cond(key, value)` returns `true` |
| `RegisterGlobalHook` | `(h Func)` | Every value access |

### Types

```go
// Func transforms a value during access.
type Func func(key string, value any) any

// Condition determines whether a conditional hook fires.
type Condition func(key string, value any) bool
```

### Thread Safety

The `Processor` is safe for concurrent use. Hooks can be registered at any time, even while other goroutines are reading configuration values. Registration uses a write lock; processing uses a read lock with snapshot copies to avoid holding the lock during hook execution.

---

## Complete Example

```go title="main.go"
package main

import (
    "context"
    "fmt"
    "strings"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    ctx := context.Background()

    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnvExpander(true),   // ${VAR} expansion (default)
        confii.WithTypeCasting(true),   // string → bool/int/float (default)
    )
    if err != nil {
        panic(err)
    }

    hp := cfg.HookProcessor()

    // Key hook: normalize app name
    hp.RegisterKeyHook("app.name", func(key string, value any) any {
        return strings.ToLower(strings.ReplaceAll(value.(string), " ", "-"))
    })

    // Condition hook: mask passwords
    hp.RegisterConditionHook(
        func(key string, value any) bool {
            return strings.Contains(key, "password") ||
                strings.Contains(key, "secret")
        },
        func(key string, value any) any {
            return "****"
        },
    )

    // Global hook: audit logging
    hp.RegisterGlobalHook(func(key string, value any) any {
        fmt.Printf("[audit] accessed: %s\n", key)
        return value
    })

    // Access values -- hooks fire automatically
    name, _ := cfg.Get("app.name")
    fmt.Println("App:", name)

    password, _ := cfg.Get("database.password")
    fmt.Println("Password:", password) // "****"
}
```
