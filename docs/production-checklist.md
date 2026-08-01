# Production Checklist

Use this checklist before deploying an application that uses Confii.

## Startup and Context

- Use `NewWithContext` when startup should inherit deployment cancellation,
  tracing, or deadlines.
- Configure `startup.timeout` in `.confii.yaml` for the fallback startup
  deadline.
- Configure `runtime.timeout` for convenience methods that do not receive an
  explicit context.
- Call `Close` during shutdown so closeable providers and watchers are
  released.

## Environment and Sources

- Prefer `environment_strategy: named_files` for new services.
- Keep one canonical environment name set: for example `development`,
  `staging`, `production`.
- Do not rely on `dev`/`prod` aliases unless those are the actual configured
  environment names and filenames.
- Use `confii plan` in CI or deployment preflight to confirm the selected
  environment and source order.
- Avoid accidental hybrid environment models. Use `hybrid` only for controlled
  migrations with an explicit conflict policy.

## Validation

- Enable `validate_on_load`.
- Enable `strict_validation` for typed configuration.
- Use `validate` tags for field-level constraints.
- Add JSON Schema when other tooling or non-Go producers need the same
  contract.
- Add custom validators for deployment policy, cross-field checks, or
  environment-specific invariants.

## Secrets

- Use explicit provider routing for mixed backends:
  `${secret@provider:key}`.
- Declare `sensitive_paths` for values that are sensitive even if they are not
  written as secret placeholders.
- Never include credentials in loader `Source()` strings, provider names, or
  error messages.
- Run `confii connections test` before deployment when using cloud or Vault
  providers.
- Prefer provider default credential chains or workload identity over static
  credentials.

## Operations

- Use `ReloadWithContext`, `ExtendWithContext`, and `RefreshSecretsWithContext`
  for runtime I/O.
- Treat reload failures as rejected candidates; the previous snapshot remains
  active.
- Use `DetectDrift` or `confii diff` for deployment verification where
  configuration drift matters.
- Enable events or metrics when operational visibility is needed.

## Debugging and Exports

- Use `Explain`, `Layers`, `SourcePlan`, and `confii debug` to inspect values.
- Protect exported effective configuration. It can contain resolved secrets.
- Keep debug reports and generated exports out of public logs unless redaction
  has been reviewed.

## Supply Chain

- Pin Confii modules consistently.
- For cloud support, keep `confii-go/v2`, `loader/cloud/v2`, and
  `secret/cloud/v2` on compatible released versions.
- Use build tags intentionally: `aws`, `azure`, `gcp`, `vault`, `ibm`.
