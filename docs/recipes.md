# Recipes

These are task-oriented flows. Each recipe links to the deeper reference page
when you need more detail.

## Load Defaults Plus an Environment File

Use the named-files model for new projects:

```yaml title=".confii.yaml"
default_environment: development
env_switcher: APP_ENV
environment_strategy: named_files
sources:
  - type: environment_files
    search_paths: [config]
    default_file: default.yaml
    environment_file: "{environment}.yaml"
```

```text
APP_ENV=production
  config/default.yaml
  + config/production.yaml
```

The selected environment file is a higher-precedence layer and is merged with
the configured merge strategy.

See [Environment Resolution](environment.md).

## Override a File Value With an Environment Variable

```yaml title=".confii.yaml"
env_prefix: MYAPP
```

```bash
MYAPP_SERVER__PORT=9090 go run .
```

`MYAPP_SERVER__PORT` maps to `server.port` and is applied after declared
sources.

See [Sources](sources.md#environment-variables).

## Preview the Exact Load Plan

```bash
confii plan
APP_ENV=production confii plan
APP_ENV=production confii plan --json
```

Use this before deployments to confirm environment selection, source order, and
mixed-model conflicts.

See [CLI](cli.md#plan).

## Explain a Surprising Value

```bash
confii explain production --key database.host
```

In Go:

```go
info := cfg.Explain("database.host")
fmt.Printf("%+v\n", info)
```

See [Introspection](introspection.md).

## Add a Custom Sensitive Path

```yaml title=".confii.yaml"
sensitive_paths:
  - database.password
  - token
  - cloud.api_key
```

Confii redacts these paths in `RedactedDict`, `ExportRedacted`, `Explain`,
`Schema`, `GenerateDocs`, source and conflict inspection, and diffs.

It does **not** redact them in `PrintDebugInfo` or `ExportDebugReport`, whose
own documentation calls their output sensitive operational data, nor in a saved
version record, which stores the materialized value alongside the sensitivity
metadata. Treat those three as carrying secrets.

See [Secrets](secrets.md) and [Introspection](introspection.md).

## Validate Before Publication

```yaml title=".confii.yaml"
validate_on_load: true
strict_validation: true
```

```go
type AppConfig struct {
    Server struct {
        Port int `confii:"port" validate:"required,min=1,max=65535"`
    } `confii:"server"`
}

cfg, err := confii.New[AppConfig]()
```

Invalid startup, reload, extension, override, or mutation candidates are
rejected before publication.

See [Validation](validation.md).

## Add a Runtime Tenant Overlay

```go
err := cfg.ExtendWithContext(ctx, loader.NewYAML("tenants/acme.yaml"))
if err != nil {
    return err
}
```

`Extend` registers the new loader for future reloads and publishes only after
the candidate passes materialization and validation.

See [Lifecycle](lifecycle.md#extend).

## Test With Temporary Overrides

```go
restore, err := cfg.Override(map[string]any{
    "server.port": 18080,
})
require.NoError(t, err)
defer restore()
```

Use `Override` for scoped tests. Use a fresh config with `WithWorkingDir` for
fixture-based tests.

See [Testing](testing.md).

## Resolve Secrets From a Custom Store

```go
resolver := secret.NewResolver(store, secret.WithCache(true))
cfg, err := confii.New[AppConfig](
    confii.WithSecretResolver(resolver),
)
```

Config value:

```yaml
database:
  password: ${secret:database/password}
```

See [Extensibility](extensibility.md#custom-secret-stores) and
[Secrets](secrets.md).

## Share Structured Values Across Files

Enable structured references when one file owns reusable structured values and
another source should read a specific field:

```go
cfg, err := confii.New[AppConfig](
    confii.WithWorkingDir("/srv/app"),
    confii.WithStructuredResolver(true),
)
```

```yaml title="shared.yaml"
server:
  port: 8080
  token: ${secret:service/token}
```

```yaml title="config.yaml"
server:
  port: ${yaml:shared.yaml#server.port}
  token: ${yaml:shared.yaml#server.token}
```

The reference is resolved before secret resolution, so the token is still
resolved through the configured secret provider.

See [Sources](sources.md#value-references).

## Add a Custom Value Resolver

```go
cfg, err := confii.New[AppConfig](
    confii.WithValueResolver("tenant", func(ctx context.Context, req hook.ResolverRequest) (any, error) {
        if err := ctx.Err(); err != nil {
            return nil, err
        }
        return lookupTenantValue(ctx, req.Target, req.Fragment)
    }),
)
```

```yaml
tenant_name: ${tenant:acme#display_name}
```

See [Extensibility](extensibility.md#custom-value-resolvers).

## Detect Drift in CI

```go
baseline := loadExpectedConfig()
drifts, err := cfg.DetectDrift(baseline)
if err != nil {
    return err
}
if len(drifts) > 0 {
    return fmt.Errorf("configuration drift detected")
}
```

See [Diff & Drift](diff.md).

## Generate Configuration Documentation

```bash
confii docs production -f markdown -o CONFIG.md
```

In Go:

```go
doc, err := cfg.GenerateDocs("markdown")
```

See [Export](export.md).
