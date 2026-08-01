# Learning Paths

Use this page to choose the shortest route through the documentation. Confii is
broad; most users do not need every feature on day one.

## I Want Typed YAML Config

1. [Quick Start](quickstart.md)
2. [Configuration](configuration.md)
3. [Accessing Values](access.md)
4. [Validation](validation.md)

Outcome: a `Config[T]` that loads local files and returns a typed model.

## I Want Environment-Specific Config

1. [Mental Model](mental-model.md)
2. [Environment Resolution](environment.md)
3. [Merging](merging.md)
4. [CLI plan command](cli.md#plan)

Outcome: `config/default.yaml` plus selected environment overrides, with clear
precedence and inspection.

## I Want Secrets

1. [Secrets](secrets.md)
2. [Configuration](configuration.md)
3. [Troubleshooting](troubleshooting.md#secrets-and-providers)
4. [Production Checklist](production-checklist.md)

Outcome: eager secret resolution with explicit providers, redaction, and
startup failure for missing required secrets.

## I Want Cloud Configuration

1. [Installation](installation.md)
2. [Sources](sources.md#cloud-loaders)
3. [Secrets](secrets.md#cloud-stores)
4. [Examples](examples.md#cloud-and-full-application-examples)

Outcome: opt-in cloud modules and build tags with S3, SSM, Vault/OpenBao, or
cloud secret stores.

## I Am Migrating From Another Config Library

1. [Mental Model](mental-model.md)
2. [Configuration](configuration.md)
3. [Sources](sources.md)
4. [Environment Resolution](environment.md)
5. [Migrate to v2](v2-migration.md)

Outcome: a gradual move from ad hoc loaders to a self-configured Confii
project.

## I Am Building a Production Service

1. [Quick Start](quickstart.md)
2. [Production Checklist](production-checklist.md)
3. [Context & Cancellation](context.md)
4. [Validation](validation.md)
5. [Secrets](secrets.md)
6. [Introspection](introspection.md)
7. [CLI](cli.md)

Outcome: bounded startup, validation before publication, explicit secret
routing, diagnostics, and deployment preflight commands.

## I Need to Customize Confii

1. [Extensibility](extensibility.md)
2. [Sources](sources.md#custom-loaders)
3. [Secrets](secrets.md#imperative-resolver-wiring)
4. [Hooks](hooks.md)
5. [Validation](validation.md#application-defined-validators)
6. [Architecture](ARCHITECTURE.md)

Outcome: custom loaders, stores, validators, hooks, exporters, and provider
registrations that still participate in Confii's transaction model.

## I Am Debugging a Broken Setup

1. [Troubleshooting](troubleshooting.md)
2. [CLI](cli.md)
3. [Introspection](introspection.md)
4. [Errors](errors.md)

Outcome: inspect source order, explain values, classify errors, and avoid
guessing which source won.
