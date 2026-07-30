# Context, cancellation, and operation lifecycles

Confii treats `context.Context` as the control plane for work, not as stored
configuration data. Contexts bound loader calls, cloud authentication, secret
reads, composition, hooks, validation, reloads, extensions, exports, diffs,
and runtime materialization.

## Construction

Use `New` for a bounded, self-contained startup:

```go
cfg, err := confii.New[AppConfig]()
```

When the caller supplies no deadline, Confii applies `startup.timeout`
(`60s` by default). Use `NewWithContext` to inherit cancellation, trace values, or
an existing deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

cfg, err := confii.NewWithContext[AppConfig](ctx)
```

An existing deadline is never extended or replaced. Cancellation is terminal:
`on_error: warn` and `on_error: ignore` cannot convert it into success.

## Runtime operations

Prefer context-aware APIs whenever an operation may invoke hooks, providers,
or I/O:

```go
err = cfg.ReloadWithContext(ctx)
err = cfg.ExtendWithContext(ctx, loader)
err = cfg.RefreshSecretsWithContext(ctx)
err = cfg.SetWithContext(ctx, "feature.enabled", true)
restore, err := cfg.OverrideWithContext(ctx, overrides)
model, err := cfg.TypedWithContext(ctx)
changes, err := cfg.DiffWithContext(ctx, other)
```

Convenience methods such as `Reload`, `Extend`, `RefreshSecrets`, `Set`,
`Override`, `Typed`, and `Diff` use an
implicit context bounded by `runtime.timeout` (`30s` by default). Configure it
with `WithOperationTimeout`; explicit `*WithContext` APIs retain their caller
deadline. The suffix is deliberate: `SetWithContext` means “perform Set using
this operation context,” never “store or replace a context on Config.”

The naming contract is uniform across the paired public API:

| Bounded convenience operation | Caller-controlled operation |
| --- | --- |
| `confii.New(...)` | `confii.NewWithContext(ctx, ...)` |
| `builder.Build()` | `builder.BuildWithContext(ctx)` |
| `cfg.Get(key)` | `cfg.GetWithContext(ctx, key)` |
| `cfg.ToDict()` | `cfg.ToDictWithContext(ctx)` |
| `cfg.Typed()` | `cfg.TypedWithContext(ctx)` |
| `cfg.Set(key, value)` | `cfg.SetWithContext(ctx, key, value)` |
| `cfg.Override(values)` | `cfg.OverrideWithContext(ctx, values)` |
| `cfg.Reload(...)` | `cfg.ReloadWithContext(ctx, ...)` |
| `cfg.Extend(loader)` | `cfg.ExtendWithContext(ctx, loader)` |
| `cfg.RefreshSecrets()` | `cfg.RefreshSecretsWithContext(ctx)` |
| `cfg.Export(format)` | `cfg.ExportWithContext(ctx, format)` |
| `cfg.Diff(other)` | `cfg.DiffWithContext(ctx, other)` |
| `cfg.DetectDrift(intended)` | `cfg.DetectDriftWithContext(ctx, intended)` |
| `cfg.SaveVersion(metadata)` | `cfg.SaveVersionWithContext(ctx, metadata)` |
| `cfg.RollbackToVersion(id)` | `cfg.RollbackToVersionWithContext(ctx, id)` |
| `composer.Compose(...)` | `composer.ComposeWithContext(ctx, ...)` |
| `composer.ComposeWithDependencies(...)` | `composer.ComposeWithDependenciesWithContext(ctx, ...)` |
| `watch.New(...)` | `watch.NewWithContext(ctx, ...)` |
| `cloud.NewHashiCorpVault(...)` | `cloud.NewHashiCorpVaultWithContext(ctx, ...)` |
| `cloud.NewOpenBao(...)` | `cloud.NewOpenBaoWithContext(ctx, ...)` |

Registration and dispatch follow the same vocabulary:
`OnChangeWithContext`, `OnWithContext`, `OffWithContext`, and
`EmitWithContext`.

Lower-level contracts that always perform caller-owned work continue to require
a context: loader `Load`, hook `Process`, secret-store operations, and provider
authentication.

Reload and Extend build private candidates while performing I/O. Concurrent
readers continue to receive the last complete snapshot; publication uses a
short lock after validation. If another mutation wins the commit race, Confii
rebuilds the candidate rather than overwriting that mutation.

## Observability

Use context-aware callbacks and event listeners to preserve trace and request
correlation:

```go
cfg.OnChangeWithContext(func(ctx context.Context, key string, oldValue, newValue any) {
    span := trace.SpanFromContext(ctx)
    span.AddEvent("configuration changed")
})

cfg.EnableEvents().OnWithContext("reload", func(ctx context.Context, args ...any) {
    logger.InfoContext(ctx, "configuration reloaded")
})
```

Context-free `OnChange` and `On` listeners remain available.

## Resource ownership

Call `Close` when the Config is no longer needed:

```go
cfg, err := confii.New[AppConfig](confii.WithDynamicReloading(true))
if err != nil { /* handle */ }
defer cfg.Close()
```

`Close` is idempotent. It stops watchers and closes owned loaders, managed
resolvers, and declaratively created providers that implement `Close() error`.
The final snapshot remains readable, while mutations return
`confii.ErrConfigClosed`.

## Secret concurrency

Startup and refresh resolve independent top-level configuration branches in
parallel. Bound provider pressure with:

```yaml
secret_resolution_concurrency: 4
```

or `confii.WithSecretResolutionConcurrency(4)`. Duplicate references to the
same provider/key/version are still coalesced within one materialization pass.
