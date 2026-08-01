# FAQ

## How is Confii different from Viper or Koanf?

Confii provides a **complete configuration lifecycle**, not just loading and reading. While Viper and Koanf handle the basics well, Confii adds:

- **Per-path merge strategies** (7 strategies) -- Viper has [known deep merge issues](https://github.com/spf13/viper/issues/181), Koanf only supports global strategy
- **Secret management** -- `${secret:key}` placeholder resolution from AWS, Azure, GCP, and Vault with caching
- **Source tracking** -- `Explain("database.host")` tells you exactly which file a value came from and how many times it was overridden
- **Diff & drift detection** -- compare configs, detect unintended changes from a baseline
- **Versioning with rollback** -- snapshot, compare, and restore config state
- **Observability** -- access metrics, event emission, change callbacks
- **Config composition** -- Hydra-style `_include` and `_defaults` with cycle detection
- **Type-safe generics** -- `Config[T]` with `Typed()` returning `*T`
- **Thread safety** -- synchronized Config instances and concurrency-safe process registries/caches (Viper has [known concurrency issues](https://github.com/spf13/viper/issues/268))

If you only need to read a YAML file and access values, Viper or Koanf may be sufficient. If you need production-grade config management with auditing, drift detection, and secret resolution, Confii fills that gap.

---

## Is Confii thread-safe?

Yes. Multiple goroutines can safely call read and lifecycle methods. Reload and
Extend perform loader/provider I/O against private candidates, so readers keep
seeing the last complete snapshot and are blocked only for the final atomic
publish. Set, Override, refresh, and version rollback are transactional writes.

```go
// Safe to use from multiple goroutines
go func() {
    val, _ := cfg.Get("database.host")
    fmt.Println(val)
}()
go func() {
    val, _ := cfg.Get("database.port")
    fmt.Println(val)
}()
```

---

## What Go version is required?

Go **1.25+** is required by the current module manifests. Cloud integrations live in separate opt-in modules so applications that use only the core do not download their SDK dependency graphs.

---

## How do I add a custom loader?

Implement the `Loader` interface:

```go
type Loader interface {
    Source() string
    Load(ctx context.Context) (map[string]any, error)
}
```

Example custom loader:

```go
type ConsulLoader struct {
    address string
    prefix  string
}

func (l *ConsulLoader) Source() string { return "consul://" + l.address + "/" + l.prefix }

func (l *ConsulLoader) Load(ctx context.Context) (map[string]any, error) {
    // Fetch from Consul KV store
    client, err := consul.NewClient(consul.DefaultConfig())
    if err != nil {
        return nil, err
    }

    pairs, _, err := client.KV().List(l.prefix, nil)
    if err != nil {
        return nil, err
    }

    result := make(map[string]any)
    for _, pair := range pairs {
        key := strings.TrimPrefix(pair.Key, l.prefix+"/")
        key = strings.ReplaceAll(key, "/", ".")
        if err := configmap.Set(result, key, string(pair.Value)); err != nil {
            return nil, fmt.Errorf("map Consul key %q: %w", key, err)
        }
    }
    return result, nil
}
```

`configmap.Set` creates nested maps from Confii's dot-separated key paths and
returns typed errors for empty paths, nil maps, and scalar/map conflicts. The
same package provides `Get`, `Has`, and deterministic, fully qualified `Keys`
for custom loaders, validators, and exporters.

Then use it like any other loader:

```go
cfg, err := confii.NewWithContext[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("defaults.yaml"),
        &ConsulLoader{address: "localhost:8500", prefix: "myapp"},
    ),
)
```

---

## How do I add a custom secret store?

Implement the `SecretStore` interface:

```go
type SecretStore interface {
    GetSecret(ctx context.Context, key string, opts ...SecretOption) (any, error)
    SetSecret(ctx context.Context, key string, value any, opts ...SecretOption) error
    DeleteSecret(ctx context.Context, key string, opts ...SecretOption) error
    ListSecrets(ctx context.Context, prefix string) ([]string, error)
}
```

Minimal read-focused example:

```go
type RedisSecretStore struct {
    client *redis.Client
}

func (s *RedisSecretStore) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
    val, err := s.client.Get(ctx, "secrets:"+key).Result()
    if errors.Is(err, redis.Nil) {
        return nil, fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
    }
    if err != nil {
        return nil, fmt.Errorf("%w: read %s: %v", confii.ErrSecretStore, key, err)
    }
    return val, nil
}

func (s *RedisSecretStore) SetSecret(ctx context.Context, key string, value any, opts ...confii.SecretOption) error {
    return s.client.Set(ctx, "secrets:"+key, value, 0).Err()
}

func (s *RedisSecretStore) DeleteSecret(ctx context.Context, key string, opts ...confii.SecretOption) error {
    return s.client.Del(ctx, "secrets:"+key).Err()
}

func (s *RedisSecretStore) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
    return nil, fmt.Errorf("%w: listing is not implemented", confii.ErrSecretStore)
}
```

Register it with the secret resolver:

```go
store := &RedisSecretStore{client: redisClient}
resolver := secret.NewResolver(store, secret.WithCache(true))
cfg, err := confii.NewWithContext[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithSecretResolver(resolver),
)
```

See [Extensibility](extensibility.md#custom-secret-stores) for the full
contract, error semantics, and testing checklist.

---

## Can I use Confii without any config files?

Yes. Environment variables can be the sole configuration source:

```go
cfg, err := confii.NewWithContext[any](ctx,
    confii.WithLoaders(loader.NewEnvironment("APP")),
)

// APP_DATABASE_HOST=localhost -> cfg.Get("database.host")
```

Or set values programmatically:

```go
cfg, err := confii.NewWithContext[any](ctx)
cfg.Set("database.host", "localhost")
cfg.Set("database.port", 5432)
```

Or use the system environment fallback to automatically check OS env vars when a key is not found:

```go
cfg, err := confii.NewWithContext[any](ctx,
    confii.WithSysenvFallback(true),
    confii.WithEnvPrefix("APP"),
)

// If cfg.Get("database.host") is not found in config,
// it checks os.Getenv("APP_DATABASE_HOST")
```

---

## How does environment resolution work?

When you set an environment (e.g., `WithEnv("production")`), Confii looks for `default` and environment-specific top-level sections in your config:

```yaml
default:
  database:
    host: localhost
    port: 5432

production:
  database:
    host: prod-db.example.com
```

Confii merges `default` first, then overlays the active environment's section. The result is a flat config without the `default`/`production` wrappers:

```go
cfg.Get("database.host") // "prod-db.example.com" (from production)
cfg.Get("database.port") // 5432 (inherited from default)
```

If no `default` or environment section exists, the config is used as-is.

---

## What happens if a source is missing?

It depends on the error policy:

| Policy | Behavior |
|--------|----------|
| `ErrorPolicyRaise` (default) | Returns an error from `New` or `Reload` |
| `ErrorPolicyWarn` | Logs a warning and continues with remaining sources |
| `ErrorPolicyIgnore` | Silently skips the source |

```go
// Continue loading even if some sources are missing
cfg, err := confii.NewWithContext[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("required.yaml"),
        loader.NewYAML("optional-overrides.yaml"),
    ),
    confii.WithOnError(confii.ErrorPolicyWarn),
)
```

!!! tip "Optional sources"
    Use `ErrorPolicyWarn` when you have optional override files that may not exist in all environments.

---

## How do I test with Confii?

Use `Override` for temporary test-scoped config changes:

```go
func TestWithOverrides(t *testing.T) {
    restore, err := cfg.Override(map[string]any{
        "database.host": "localhost",
        "database.port": 15432,
        "cache.enabled": false,
    })
    require.NoError(t, err)
    defer restore()

    // All assertions run with overridden config
    host, _ := cfg.Get("database.host")
    assert.Equal(t, "localhost", host)
}
```

Or create a fresh config instance per test:

```go
func TestDatabaseConfig(t *testing.T) {
    cfg, err := confii.New[any](confii.WithLoaders(loader.NewYAML("testdata/test-config.yaml")),
        confii.WithEnv("test"),
    )
    require.NoError(t, err)

    host, _ := cfg.Get("database.host")
    assert.Equal(t, "test-db", host)
}
```

For programmatic test configs without files:

```go
func TestWithInlineConfig(t *testing.T) {
    cfg, err := confii.New[any]()
    require.NoError(t, err)

    cfg.Set("feature.enabled", true)
    cfg.Set("timeout", 30)

    enabled, _ := cfg.GetBool("feature.enabled")
    assert.True(t, enabled)
}
```
