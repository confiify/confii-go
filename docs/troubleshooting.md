# Troubleshooting

Start here when the effective configuration does not match what you expected.
Prefer inspection commands over guessing.

## First Commands to Run

```bash
confii env
confii env list
confii plan
confii explain --key database.host
```

Use `APP_ENV=production` or another selector prefix when debugging a specific
environment:

```bash
APP_ENV=production confii plan
APP_ENV=production confii explain --key database.host
```

## Environment Selection

### My environment file was not loaded

Check the selected environment:

```bash
confii env
```

For named files, the selected environment must match the filename exactly.
`production` selects `production.yaml`; `prod` selects `prod.yaml`.

Also confirm `.confii.yaml` declares `environment_files`:

```yaml
sources:
  - type: environment_files
    search_paths: [config]
    default_file: default.yaml
    environment_file: "{environment}.yaml"
```

### `.confii.production.yaml` changed the wrong thing

`.confii.production.yaml` configures Confii itself. It does not contain normal
application values unless you intentionally put source declarations there.

Application values belong in `config/production.yaml` or another declared
loader source.

### `APP_ENV=prod` did not use production

Confii does not alias environment names. Use one canonical name consistently:

```text
APP_ENV=production -> .confii.production.yaml + config/production.yaml
APP_ENV=prod       -> .confii.prod.yaml + config/prod.yaml
```

## Source Precedence

### A value came from the wrong file

Run:

```bash
confii plan
confii explain --key path.to.value
```

Later sources override earlier sources. Environment variables configured with
`env_prefix` are intentionally high precedence.

### My environment variable did not override a key

Confirm the prefix and key mapping:

```yaml
env_prefix: APP
```

```text
APP_DATABASE__HOST -> database.host
APP_SERVER__PORT   -> server.port
```

If you enabled `sysenv_fallback`, remember it is a read fallback for missing
keys. It does not add a layer to the published snapshot.

## Secrets and Providers

### Why does `confii get` or `confii plan` contact Vault?

Commands that materialize the effective configuration resolve effective secret
references. That is deliberate: Confii publishes a ready snapshot and fails
startup if required secrets are missing.

Use `confii connections test` to preflight providers without printing secret
values.

### Cloud secret package has no buildable files

Cloud providers are opt-in through build tags. Import the provider module and
build with the needed tag:

```go
import _ "github.com/confiify/confii-go/secret/cloud/v2"
```

```bash
go run -tags "aws,vault" .
```

### A secret value appears redacted

Redaction is expected in diagnostics for secret references and declared
`sensitive_paths`. Read the value from the application snapshot only when the
application genuinely needs it.

## Validation and Reload

### Reload failed but the app still uses old values

That is the intended safety model. Reload builds a private candidate and
publishes only if loading, hooks, secret resolution, and validation succeed.

Check the returned error and fix the source or validator. Readers continue to
observe the previous complete snapshot.

### My custom validator changed the map but nothing changed

Confii gives validators an independent copy of the candidate. Validators must
return errors for violations; they are not a transformation mechanism. Use
hooks for transformations.

## Working Directory

### Confii reads the wrong `.confii.yaml`

Self-config discovery is rooted at the working directory unless you pass
`WithWorkingDir`:

```go
cfg, err := confii.New[AppConfig](
    confii.WithWorkingDir("/path/to/project"),
)
```

This is especially useful in tests and tools that run from another directory.

## Error Categories

Use `errors.Is` and `errors.As`, not string matching:

```go
if errors.Is(err, confii.ErrConfigValidation) {
    // validation failed
}

var cfgErr *confii.ConfigError
if errors.As(err, &cfgErr) {
    slog.Error("config failed", "op", cfgErr.Op, "code", cfgErr.Code)
}
```

See [Errors](errors.md).
