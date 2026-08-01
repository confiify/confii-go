# Glossary

This page defines terms used throughout the Confii documentation.

| Term | Meaning |
| --- | --- |
| Application config | Values consumed by your application and decoded into `Config[T]` |
| Candidate | A private configuration state prepared by startup, reload, extension, mutation, override, refresh, or rollback before publication |
| Composition | Processing `_include`, `_defaults`, and `_merge_strategy` directives before merge |
| Config[T] | The published configuration object, parameterized by your typed model |
| Control plane | Confii's own settings: sources, environment selector, providers, validation, timeouts |
| Environment | The active name such as `development` or `production` |
| Environment file | A named-file override such as `config/production.yaml` |
| Environment section | A top-level map such as `production:` inside a sectioned file |
| Hook | A transformation callback that runs before validation and publication |
| Layer | One source contribution in the ordered merge stack |
| Loader | Interface that reads one configuration source into `map[string]any` |
| Materialization | Expansion, type casting, secret resolution, and hook processing that creates effective values |
| Named files | Environment model using `default.yaml` plus `<environment>.yaml` |
| Publication | Atomic replacement of the visible snapshot after a candidate succeeds |
| Secret provider | A named declarative secret backend in `.confii.yaml` |
| Secret resolver | Component that replaces `${secret:...}` placeholders through a store |
| Secret store | Backend implementation that reads and writes secret values |
| Sectioned file | Environment model using one file with `default` and environment sections |
| Self-config | `.confii.yaml` and optional `.confii.<environment>.yaml` files that configure Confii |
| Snapshot | The complete effective configuration visible to readers |
| Source | A file, environment prefix, HTTP endpoint, cloud object, or custom loader |
| SourcePlan | The resolved environment model and ordered source/layer plan |
| Validator | Application rule that accepts or rejects a materialized candidate |

## Common Distinctions

| Do not confuse | With |
| --- | --- |
| `.confii.production.yaml` | `config/production.yaml` |
| `env_prefix` loader | `sysenv_fallback` read fallback |
| Hook | Validator |
| `Typed()` shared snapshot | `TypedCopy()` detached copy |
| Reload existing sources | Extend with a new source |
| Redacted diagnostics | Application access to the resolved value |
