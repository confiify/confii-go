# Hook System

Confii v2 treats hooks as configuration transformations. Hooks are registered
before construction, frozen into the materialization plan, and run before a
snapshot is published:

![Confii configuration startup flow](assets/configuration-flow.svg)

```text
load → compose → select environment → transform hooks → resolve secrets
     → validate → publish immutable snapshot
```

`Get`, `Typed`, `ToDict`, and `Export` all read the same materialized snapshot.
They do not rerun hooks or contact a secret provider. `Set`, `Override`,
`Extend`, `Reload`, and `RefreshSecrets` build a candidate through the same
pipeline and publish it only when every transformation and validation succeeds.

This is especially important for typed configuration: a field expression such
as `appConfig.Server.Host` observes the value produced for `server.host` during
materialization. No generated getter or repeated `cfg.Get` call is required.

## Registering hooks

Four construction options correspond to the four transformation selectors:

```go
cfg, err := confii.New[AppConfig](
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithKeyHook("app.name", func(ctx context.Context, key string, value any) (any, error) {
        return strings.ToUpper(value.(string)), nil
    }),
    confii.WithValueHook("PLACEHOLDER", func(ctx context.Context, key string, value any) (any, error) {
        return "resolved", nil
    }),
    confii.WithConditionHook(
        func(ctx context.Context, key string, value any) (bool, error) {
            return strings.HasSuffix(key, ".host"), nil
        },
        func(ctx context.Context, key string, value any) (any, error) {
            return strings.TrimSpace(value.(string)), nil
        },
    ),
    confii.WithGlobalHook(func(ctx context.Context, key string, value any) (any, error) {
        return value, nil
    }),
)
```

The builder exposes the matching `WithKeyHook`, `WithValueHook`,
`WithConditionHook`, and `WithGlobalHook` methods.

Hook order for every leaf is:

1. exact key hooks;
2. matching value hooks;
3. condition hooks whose predicates return `true`;
4. global hooks.

Registration order is preserved within each category. Every callback receives
the full dotted key path and the startup or mutation context. Returning an
error aborts the candidate transaction; no partial transformed state becomes
visible.

## Secret resolvers

Imperative secret resolution is also construction-time:

```go
resolver := secret.NewResolver(store, secret.WithCache(true))

cfg, err := confii.New[AppConfig](
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithSecretResolver(resolver),
)
```

`WithSecretResolver` lets `RefreshSecrets` clear the resolver cache before
building a fresh candidate. `WithSecretHook(resolver.Hook())` is available when
cache management is owned elsewhere.

Missing secrets and provider failures always return errors in v2. Optionality
belongs in an explicit schema or reference model; unresolved placeholders are
never silently published.

## Value Resolvers

Value resolvers are specialized global hooks for `${scheme:...}` expressions.
They run after environment expansion and before type casting, application hooks,
and secret resolution. That order lets a referenced file or self field contain
`${secret:...}` and still have the secret resolved by the normal secret phase.

Built-in resolver families are opt-in:

```go
cfg, err := confii.New[AppConfig](
    confii.WithWorkingDir("/srv/app"),
    confii.WithStructuredResolver(true), // ${json:path#field}, ${yaml:path#field}, self refs
    confii.WithFileResolver(true),       // ${file:path}
    confii.WithURLResolver(false),       // keep network I/O off unless needed
    confii.WithCommandResolver(false),   // keep shell execution off unless needed
)
```

Custom resolvers use `WithValueResolver`:

```go
cfg, err := confii.New[AppConfig](
    confii.WithValueResolver("upper", func(ctx context.Context, req hook.ResolverRequest) (any, error) {
        return strings.ToUpper(req.Target), nil
    }),
)
```

Configuration:

```yaml
label: ${upper:hello}
```

If the reference is the complete scalar, the resolver's Go value is preserved.
If it is embedded inside a larger string, Confii stringifies the returned value.
Unknown schemes are left unchanged so another hook or application layer may
handle them.

## Runtime read behavior

Normal reads are deliberately side-effect free. If an application needs
request-specific authorization, masking, or telemetry, perform that in an
application read layer rather than a transformation hook. This keeps the
configuration snapshot consistent across scalar, map, typed, export, and
introspection APIs.

## Standalone processor

The `hook` package still exposes `hook.NewProcessor()` for applications that
need the transformation engine independently of `Config`. A Config's internal
processor is not mutable after construction.
