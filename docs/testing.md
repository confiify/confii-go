# Testing Applications With Confii

Confii is designed so tests can use the same publication model as production
without contacting production services.

## Unit Tests With Inline Sources

Use a tiny custom loader for direct maps:

```go
type mapLoader map[string]any

func (l mapLoader) Source() string { return "test:map" }
func (l mapLoader) Load(context.Context) (map[string]any, error) {
    return map[string]any(l), nil
}

cfg, err := confii.New[AppConfig](
    confii.WithLoaders(mapLoader{
        "server": map[string]any{"port": 8080},
    }),
)
```

## Fixture Projects With Self-Config

Use `WithWorkingDir` when your test creates `.confii.yaml` and `config/*.yaml`
under a temp directory:

```go
dir := t.TempDir()
// write .confii.yaml and config/default.yaml

cfg, err := confii.New[AppConfig](
    confii.WithWorkingDir(dir),
)
```

This avoids accidentally reading the process working directory.

## Temporary Overrides

Use `Override` for scoped behavior:

```go
restore, err := cfg.Override(map[string]any{
    "server.port": 18080,
})
require.NoError(t, err)
defer restore()
```

The restore function is idempotent and returns the published snapshot to its
previous state.

## Avoid Real Cloud Calls

Use `secret.DictStore`, environment-backed stores, or local emulators:

```go
store := secret.NewDictStore(map[string]any{
    "database/password": "test-password",
})
resolver := secret.NewResolver(store)

cfg, err := confii.New[AppConfig](
    confii.WithSecretResolver(resolver),
)
```

For cloud loader behavior, prefer LocalStack or test fakes that implement the
same loader/store interfaces.

## Test Validators and Hooks

Hooks transform values before validation:

```go
cfg, err := confii.New[AppConfig](
    confii.WithLoaders(mapLoader{"app": map[string]any{"name": "  demo  "}}),
    confii.WithKeyHook("app.name", trimHook),
    confii.WithValidator(policyValidator{}),
)
```

Assert both success and failure paths. A failing hook or validator must reject
the candidate and leave the previous snapshot unchanged.

## Test Reload and Extension

Use `ReloadWithContext` and `ExtendWithContext` in tests when the application
uses those paths in production:

```go
err := cfg.ExtendWithContext(ctx, loader.NewYAML("testdata/override.yaml"))
require.NoError(t, err)
```

For reload failure tests, validate that the error is returned and the previous
value is still visible.

## Drift and Export Tests

Use drift detection for golden expectations:

```go
actual, err := cfg.ToDict()
require.NoError(t, err)

diffs := diff.Compare(expected, actual)
require.Empty(t, diffs)
```

When exporting resolved configuration, remember that the output can contain
secret material. Keep golden files free of real credentials.

## Test Checklist

- Use `WithWorkingDir` for fixture directories.
- Use fake loaders or dict secret stores for unit tests.
- Assert both successful and failing candidates.
- Use `Override` for scoped temporary changes.
- Avoid relying on global process environment unless the test owns it with
  `t.Setenv`.
- Do not commit generated snapshots or secret-bearing exports.

## Local PR Readiness

Run the pull-request gate before pushing substantial changes:

```bash
make pr-check
```

It mirrors the CI failures that are practical to catch locally: DCO sign-off,
formatting, vet, golangci-lint, shuffled tests, race tests, short fuzz targets,
statement coverage, local patch coverage, and documentation validation when
docs-triggering files changed.

For larger changes, run the heavier sweep:

```bash
PR_CHECK_LEVEL=full PR_FUZZTIME=30s make pr-check
```

The full sweep adds module integrity, integration tests, cloud consumer tests,
branch coverage, reproducible builds, API compatibility, REUSE, and
supply-chain metadata checks. When CI or Codecov exposes a new failure mode,
add a local probe to `scripts/pr-check.sh` or a focused helper under `scripts/`
so the next contributor can catch it before pushing.
