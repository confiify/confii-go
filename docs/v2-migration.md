# Migrating to Confii v2

Confii v2 intentionally tightens contracts that could not be corrected safely
inside v1. Perform the following changes together when upgrading.

## 1. Update module paths

Go requires a `/v2` suffix for version 2 modules:

```bash
go get github.com/confiify/confii-go/v2@latest
go install github.com/confiify/confii-go/v2/confii@latest
```

Change core imports from `github.com/confiify/confii-go/...` to
`github.com/confiify/confii-go/v2/...`.

The optional cloud packages are independent modules, so their major suffix
comes after the nested module path:

```bash
go get github.com/confiify/confii-go/loader/cloud/v2@latest
go get github.com/confiify/confii-go/secret/cloud/v2@latest
```

For example:

```go
import (
    confii "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
    _ "github.com/confiify/confii-go/secret/cloud/v2"
)
```

## 2. Choose the constructor context model

The default constructor no longer requires context boilerplate:

```go
cfg, err := confii.New[AppConfig](confii.WithLoaders(source))
```

It uses an implicit background context and a configurable 60-second startup
deadline. Replace v1 calls that passed a context with `NewWithContext` when that
context is semantically important:

```go
cfg, err := confii.NewWithContext[AppConfig](ctx, confii.WithLoaders(source))
```

The same split applies to the builder: use `Build()` for the implicit path and
`BuildWithContext(ctx)` for explicit propagation. Configure the fallback through
`startup.timeout` in `.confii.yaml` or `WithStartupTimeout`. A caller-provided
deadline always wins, and a zero fallback disables Confii's added deadline.

All paired APIs follow that spelling in v2: the short operation uses Confii's
bounded implicit context and the `OperationWithContext` form accepts the
caller's context. For example, use `Get` / `GetWithContext`, `Set` /
`SetWithContext`, and `Typed` / `TypedWithContext`. Names ending in `Ctx` or the
ambiguous `SetContext` form are not part of the v2 API.

## 3. Use `confii` mapping tags

Replace Confii's former `mapstructure` mapping tags with the project-owned
`confii` tag:

```go
type AppConfig struct {
    LogLevel string `confii:"log_level" validate:"required,oneof=debug info warn error"`
    Internal string `confii:"-"`
}
```

The mapping options `-`, `squash`, and `remain` are supported. Validation is a
separate concern: `validate` tags remain valid and continue to be evaluated by
the struct validator.

## 4. Migrate hooks

All hooks now accept a context and return an error. The separate `FuncCtx`,
`HookCtx`, `ProcessCtx`, and `Register*HookCtx` APIs have been removed.

```go
confii.WithKeyHook("server.host",
    func(ctx context.Context, key string, value any) (any, error) {
        if err := ctx.Err(); err != nil {
            return nil, err
        }
        return strings.ToLower(value.(string)), nil
    },
)
```

Conditions follow the same rule:

```go
func(ctx context.Context, key string, value any) (bool, error) {
    return strings.HasSuffix(key, ".password"), nil
}
```

Use `Processor.Process(ctx, key, value)` and `Resolver.Hook()`. Errors and
context cancellation now propagate rather than being silently converted into
an unchanged value.

Register hooks through constructor options or the builder. The materialization
plan is frozen once construction succeeds; Config does not expose its internal
processor for post-construction mutation. `Get`, `Typed`, `ToDict`, and
`Export` read the same published values without rerunning hooks.

Value hooks are matched after key hooks run. If a key hook changes `"raw"` to
`"normalized"`, a value hook registered for `"normalized"` runs in that same
pipeline. This replaces v1's surprising preselection against the original
input.

## 5. Handle snapshot and diff errors

Snapshot APIs return errors for cancellation and operation failures:

```go
snapshot, err := cfg.ToDict()
diffs, err := cfg.Diff(other)
drifts, err := cfg.DetectDrift(intended)
```

Do not discard these errors in startup, validation, export, or drift-detection
paths. `ToDictWithContext`, `TypedWithContext`, and other context-aware reads remain available
when the caller controls cancellation or deadlines.

## 6. Make provider registration deterministic

Declarative source and secret providers are process-wide registrations.
Registering an empty name, a nil factory, or the same normalized name twice now
panics during initialization. Provider packages should register each name once,
normally from a package `init` function, and applications should avoid importing
two implementations for the same provider name.

This fail-fast behavior prevents import order from silently selecting a
different cloud or secret implementation.

## 7. Canonicalize source types

Use canonical declarative and CLI source types in v2:

| v1 spelling | v2 spelling |
| --- | --- |
| `yml` | `yaml` |
| `cfg` | `ini` |
| `envfile`, `env_file`, or `env` with a path | `dotenv` |
| `env` with a prefix or `env-vars` | `environment` |
| `environment-files` | `environment_files` |

Filename extensions remain conventional: `type: yaml` accepts `.yaml` and
`.yml`, while `type: ini` accepts `.ini` and `.cfg`. The declared type must
agree with the path, so correct contradictory declarations instead of relying
on extension-only parser selection. Also rename content rather than merely its
file extension: v2 rejects a complete JSON document declared as YAML, TOML,
INI, or dotenv and never retries it with another parser.

## 8. Update Vault auth inputs

The ordinary cloud identity adapters now delegate identity acquisition and
login construction to HashiCorp's official Vault auth packages:

- `AWSIAMAuth` uses the standard AWS credential chain and accepts `Role`,
  `Region`, `IAMServerIDHeader`, and `MountPoint`. Use
  `AWSIAMSignedRequestAuth` only when the application supplies an externally
  signed request.
- `AzureAuth` obtains managed identity and instance metadata from Azure IMDS.
  Use `AzureJWTAuth` for an externally brokered identity JWT.
- `GCPAuth` selects `gce` or `iam` through `AuthType`; IAM also requires
  `ServiceAccountEmail`. Use generic `JWTAuth` with the `gcp` mount for an
  externally supplied GCP identity JWT.
- AppRole accepts exactly one of `SecretID`, `SecretIDFile`, or `SecretIDEnv`.
  Kubernetes accepts at most one of `JWT`, `TokenPath`, or `TokenEnv`.

Declarative Vault auth uses the same distinction through `aws_iam`,
`aws_signed_request`, `azure`, `azure_jwt`, `gcp`, and `gcp_jwt`.

## 9. Update version record consumers

Version IDs are now canonical, time-sortable ULIDs. `Version.Timestamp` is a
`time.Time`; the duplicate float timestamp and `DateTime` string are gone.
Format the timestamp at the presentation boundary:

```go
fmt.Println(version.Timestamp.Format(time.RFC3339Nano))
```

`VersionManager.DiffVersions` returns `[]diff.ConfigDiff`, matching
`diff.Diff`. Replace map indexing such as `entry["path"]` with typed fields
such as `entry.Path`, `entry.Type`, and `entry.NestedDiffs`.

Persisted v1 snapshot filenames and records are not loaded by the v2 manager;
create a new v2 snapshot baseline after upgrading.

## 10. Verify the upgrade

```bash
go mod tidy
go test ./...
go vet ./...
confii --version
confii plan
confii connections test
```

If the application uses optional cloud packages, run the tests with the same
provider build tags used for its production binary.
