# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.2.1] - 2026-07-25

### Security

- Enforce at least 80% condition/branch coverage across non-example shipping
  packages using the pinned BSD-licensed Gobco v1.3.4 tool, replacing an
  incorrect assumption that Go branch coverage was not measurable.

- Upgrade the OpenTelemetry API, metric, trace, and SDK modules used by the
  opt-in cloud packages to v1.44.0, fixing GO-2026-5158 / CVE-2026-41178
  (unbounded baggage-header parsing).

### Added

- Add machine-readable OpenSSF Best Practices evidence, a public roadmap,
  consolidated quality policy, dated security review and assurance case,
  reproducible-build verification, a mandatory 90% non-example statement
  coverage gate, and per-source-file copyright/SPDX declarations.

- Add per-archive SPDX 2.3 SBOMs, machine-readable OpenVEX decisions,
  checksummed release metadata, Sigstore/SLSA provenance, and semantic SBOM
  validation through pinned `bomctl`.

- Add OpenSSF Security Insights 2.2 metadata, Fuzz Introspector reporting for
  all 11 native fuzz targets, live OpenBao Token/AppRole/KV interoperability,
  and an audit-only Minder policy profile.

- Add explicit project credential, release support/EOL, continuity, and OSPS
  Baseline Level 1–3 evidence policies, plus a hardened Cloudflare Pages
  documentation deployment.

### Changed

- Enforce API compatibility, REUSE 3.3 license compliance, DCO sign-off,
  dependency review, CodeQL, Govulncheck, secret scanning, coverage, and
  supply-chain metadata as protected merge gates.

- Update the opt-in cloud modules and security tooling to their reviewed
  dependency releases, including `google.golang.org/api` v0.290.0 and
  `github.com/goccy/go-yaml` v1.19.2.

### Fixed

- Treat TOML IEEE NaN values as equivalent configuration values in the
  self-merge fuzz invariant while retaining strict comparisons for finite
  values.

- Accept GitHub Dependabot's exact GitHub-managed author/signoff identity pair
  in DCO validation without granting a general bot exemption.

## [1.2.0] - 2026-07-24

### Added

- Opt-in `environment_files` self-config source for projects that keep
  `default` and environment overrides in separate files. It supports ordered
  search paths, configurable filename templates, required/optional roles,
  safe environment-name validation, source introspection, and reload through
  the existing loader pipeline without changing section-based single-file or
  explicit-loader behavior. Environment strategy guardrails reject accidental
  mixing with section-based sources; explicit hybrid migrations require a
  conflict policy and expose their ordered source plan through `Config.SourcePlan`
  and `confii plan`.

## [1.1.0] - 2026-07-23

This release is a defensive-hardening pass. It strengthens the
isolation, atomicity, and observability contracts the library
already documented but did not always enforce. There are no breaking
changes to the public Go API; one new public type and three new
public functions extend the self-config secret provider story.

### Added

- Independently tidy `loader/cloud` and `secret/cloud` modules, keeping the
  root module cloud-SDK-free while giving users reproducible provider SDK
  versions. Isolated consumer verification covers build, test, and vet gates.
- `dictutil.KeyCollisionError` and `dictutil.KeyCoercionError` —
  typed errors returned by `dictutil.NormalizeKeys` when a YAML map
  key cannot be projected to a canonical string or when two distinct
  source keys collide on their projection. Propagated as YAML format
  errors by the loader so operators see the exact ambiguity.
- `sourcetrack.FileSnapshot`, `(*FileTracker).Snapshot`,
  `(*FileTracker).Restore` — atomic-rollback API for the file
  tracker, used by `Config.Reload` so a failed reload restores file
  mtime/hash state alongside `envConfig`/`mergedConfig`/source
  tracker.
- `observe.(*VersionManager).Reconfigure` — in-place reconfiguration
  of an existing version manager, preserving the in-memory snapshot
  ring.
- `confii.SelfConfigSecretStore` (interface),
  `confii.SelfConfigSecretProviderFactory` (type),
  `confii.RegisterSelfConfigSecretProvider`,
  `confii.LookupSelfConfigSecretProvider` — public registry for
  self-config secret providers, following the `database/sql` driver
  pattern. Built-in providers (`env`, `dict`, `file`) register at
  init; cloud providers register via build-tagged blank import of
  `secret/cloud`.
- `dict` and `file` self-config secret providers. `file` is
  Docker / Kubernetes secret-mount style and includes a path-
  traversal guard rejecting `..`, absolute paths, and null bytes.
- `docs/ARCHITECTURE.md` — contributor guide describing the deep-
  copy engine, override stack, secret registry, and cross-cutting
  invariants.
- "Threat Model & Mitigations" section in
  [SECURITY.md](SECURITY.md).
- "Testing Philosophy" section in [README.md](README.md): negative
  tests required for new features, full suite must pass under
  `-race`.
- Reproducible cross-platform CLI archives, SHA-256 checksums, and GitHub
  artifact attestations in the tag-triggered release workflow. The CLI now
  reports its linked release through `confii --version`.
- Multi-module release, support, CODEOWNERS, and private security-reporting
  guidance for maintainers and contributors.

### Changed

- `dictutil.NormalizeKeys` signature changed from
  `func(any) any` to `func(any) (any, error)`. Callers in
  `loader/yaml.go` and the in-package `fileAutoLoader` updated to
  propagate the new error.
- `dictutil.DeepCopy` and `dictutil.DeepCopyValue` now use a
  reflection engine over every `reflect.Kind` instead of a typed
  switch over `map[string]any` and `[]any`. Typed slices, typed
  maps, byte slices, pointers, custom struct values, arrays, and
  cyclic pointer graphs are all handled. No behavioral change for
  callers that only pass JSON/YAML decoder shapes.
- `Config.Override` is now LIFO-composable. Each call pushes a frame
  onto an internal stack; the returned restore closure removes its
  own frame regardless of stack position and rebuilds live state by
  replaying remaining frames onto the captured base. Restore is
  idempotent.
- `Config.EnableVersioning` always takes effect. Previous behavior
  silently dropped the storage path and version cap when
  `Config.SaveVersion` had lazy-initialized a defaults manager
  first.
- `Tracker.GetOverrideHistory` now deep-copies each entry's `.Value`
  payload, matching the contract already in place for
  `GetSourceInfo` and `GetConflicts`.

### Fixed

- **Re-entrant hook deadlocks.** `GetCtx`, `ToDictCtx`, `ExportCtx`, and
  `TypedCtx` now snapshot configuration under lock and execute user hooks
  after releasing it. Hooks may call back into Config APIs, and slow secret
  resolution no longer blocks unrelated writers. Typed-cache publication is
  skipped when a hook changed live state while resolving the snapshot.
- **Vault 404 misclassification.** HashiCorp Vault reads now inspect the raw
  HTTP status before decoding the response body, so missing secrets map to
  `ErrSecretNotFound` even when a proxy returns a plain-text 404 body.
- **Lost shared identity in `dictutil.DeepCopy`.** The root map is now cloned
  in one visited-set traversal, preserving aliases shared by top-level keys
  and self-referential root-map cycles without retaining source references.
- **Contradictory module CI gates.** Core and cloud integrations now have
  independent module manifests, and CI verifies that every manifest is tidy
  before exercising cloud-tag builds through a consumer fixture.
- **Atomic rollback in `Config.Reload`.** A Phase 5/6/7 failure now
  restores `fileTracker` alongside `envConfig`, `mergedConfig`, and
  the source tracker. Previously the file tracker carried the
  malformed-content hash forward, so the next incremental Reload
  short-circuited at the change-detection gate and silently reported
  success on stale state.
- **Phantom resurrection in nested `Config.Override`.** Restoring a
  non-top frame no longer resurrects values from already-popped
  frames. The new stack-based restore rebuilds live state from the
  captured base.
- **Reference aliasing in `Config.Set` / `Config.Override`.**
  Defensive deep-copy now correctly isolates typed leaves
  (`[]byte`, `[]string`, `map[string]int`, custom structs, pointers)
  in addition to the JSON/YAML decoder shapes.
- **Reference aliasing in `Tracker.GetSourceInfo` and friends.**
  `SourceInfo.Value` and per-`OverrideEntry.Value` payloads are now
  deep-copied so a caller mutating a returned map cannot poison
  tracker state.
- **Type drift in `OnChange` callback dispatch.** Equality is now
  computed via `reflect.DeepEqual` rather than `fmt.Sprintf("%v",…)`
  comparison. `int(8080)` and `float64(8080)` are no longer treated
  as equal, so JSON-decoded float values replacing ints correctly
  fire the callback.
- **Lost arguments in concurrent `EnableVersioning` /
  `SaveVersion`.** The shared `sync.Once` is removed; the new path
  always honors the most recent `EnableVersioning(path, n)` call.
- **Silent map-key collisions in YAML.** `dictutil.NormalizeKeys`
  surfaces `*KeyCollisionError` for distinct typed keys that share
  a coerced string (`true` vs `"true"`, `1` vs `"1"`, `1.0` vs `1`)
  rather than silently dropping one entry.
- **Self-config startup failure for non-`env` secret providers.**
  The hard-coded "env-only" switch is replaced by a registry; built-
  in `env`/`dict`/`file` providers ship by default, cloud providers
  opt in via build-tagged blank import.
- **Silent panic recovery in `OnChange` callback dispatch.** A
  panicking callback is now logged at error level with the affected
  key, callback index, recovered value, and full
  `runtime/debug.Stack()`. Sibling callbacks continue regardless.

### Security

- Upgraded AWS S3, `golang.org/x/net`, `golang.org/x/text`, and go-jose to
  fixed releases after a full cloud-tag `govulncheck` scan found reachable
  denial-of-service and panic advisories. Both core and cloud dependency
  graphs are now scanned in CI.

- **Data race in `Tracker.ExportDebugReport`.** The routine now
  builds a deep-copied snapshot under the read lock and releases the
  lock before marshaling. The previous implementation populated the
  report with live `*SourceInfo` pointers and marshaled outside any
  lock, racing concurrent `TrackValue` mutations of `Value`,
  `SourceFile`, `LoaderType`, and `Timestamp`. Tripped
  `go test -race`.
- Path-traversal guard in the `file` self-config secret provider:
  keys containing `..`, leading `/`, or null bytes are rejected
  before any filesystem access, so adversarial config cannot read
  arbitrary files outside the configured `base_dir`.

## [1.0.0]

### Added

- Core `Config[T]` with type-safe generics and fluent builder pattern
- Multi-source loading: YAML, JSON, TOML, INI, .env, environment variables, HTTP
- Cloud loaders: AWS S3, SSM, Azure Blob, GCS, IBM COS, Git repositories
- Secret management with `${secret:key}` placeholder resolution
- Cloud secret stores: AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, HashiCorp Vault (9 auth methods)
- 6 merge strategies (replace, merge, append, prepend, intersection, union) with per-path overrides
- Hydra-style config composition via `_include` and `_defaults` directives
- Environment resolution with automatic default + environment-specific merging
- 4-type hook system (key, value, condition, global) for value transformation
- Struct tag validation via go-playground/validator and JSON Schema validation
- Full introspection: Explain(), Layers(), Schema(), source tracking, override history
- Config diff, drift detection, versioning with rollback
- File watching with incremental reload (mtime + SHA256)
- Observability: access metrics, event emission
- Documentation generation (markdown, JSON)
- Export to JSON, YAML, TOML
- Self-configuration via `.confii.yaml` auto-discovery
- CLI tool with 10 commands: load, get, validate, export, diff, debug, explain, lint, docs, migrate
- 19 runnable examples
- GitHub Actions CI/CD: test matrix, CodeQL, govulncheck, OSSF Scorecard
- Broad unit, integration, race, fuzz, and cross-platform test coverage
