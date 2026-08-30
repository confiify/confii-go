# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add `WithRejectUnknownKeys` so configuration keys the typed model does not
  declare fail the typed decode instead of leaving a zero-valued field. A
  mistyped key such as `prot` for `port` is otherwise indistinguishable from an
  intentionally absent setting. The default is `false`, preserving current
  behavior, and `Config[any]` is unaffected. The policy is also readable from
  self-configuration as `reject_unknown_keys`.
- Add `validate.Options` with `WeaklyTypedInput` and `RejectUnknownKeys`, plus
  `validate.DecodeWithOptions`, `validate.DecodeAndValidateWithOptions`, and
  `validate.NewStructValidatorWithOptions`, so callers can require decoded
  input to already carry each field's declared type. The existing `Decode`,
  `DecodeAndValidate`, and `NewStructValidator` keep weak scalar conversion.

### Fixed

- Honor `WithTypeCasting` in the typed decode path. `WithTypeCasting(false)`
  preserved loaded strings in the snapshot but `Typed` and `TypedCopy`
  converted them anyway, so the same `Config` reported different values for
  the same key. Configurations that disable conversion now fail the decode
  when a value's type does not match its field instead of converting it
  silently. The default remains `true`, so behavior is unchanged unless
  conversion was explicitly disabled.
- Correct the `StrictValidation` description in `selfconfig`, which said the
  option "rejects unknown typed-configuration fields" while it governs whether
  a validation failure is reported as an error or a warning. Undeclared keys
  are now handled by `WithRejectUnknownKeys`.
- Correct the `WithEnvExpander` and `WithTypeCasting` defaults in `llms.txt`,
  which listed both as `false` while `defaultOptions` sets both to `true`.
  `README.md` and `docs/configuration.md` were already correct.

## [2.1.0] - 2026-08-01

### Added

- Add opt-in value resolvers for `${file:path}`, `${env:NAME}`,
  `${json:path#field}`, `${yaml:path#field}`, `${json:self#field}`, and
  `${yaml:self#field}`. File and structured resolvers are rooted in the
  project directory, preserve full-value types when the placeholder is the
  entire value, and keep resolver failures transactional.
- Add explicitly disabled powerful resolvers for `${url:...}` and `${cmd:...}`
  so applications can opt in only for fully trusted configuration.
- Add custom resolver registration so applications can extend placeholder
  resolution without forking Confii.
- Add visual onboarding and concept documentation, including mental models,
  learning paths, recipes, troubleshooting, production guidance, and diagrams
  for configuration flow, source precedence, composition, secrets, lifecycle,
  and operations.
- Add local PR readiness gates with `make pr-check` and
  `make patch-coverage-check` so DCO, lint, tests, race checks, fuzz smoke
  tests, statement coverage, patch coverage, and docs checks can be run before
  GitHub CI.

### Fixed

- Make self-reference resolver context race-free by snapshotting unresolved
  environment configuration under lock before materialization.
- Improve resolver and watcher coverage, including Windows-safe filesystem
  edge tests and deterministic closed-channel watcher loop coverage.

## [2.0.0] - 2026-07-31

### Added

- Add a configurable trailing-edge reload debounce (`reload_debounce` and
  `WithReloadDebounce`) with a 150ms default. Editor write bursts now produce
  one watcher-driven reload, zero retains immediate behavior, and shutdown
  cancels pending work.
- Add explicit sensitivity classification through `sensitive_paths` and
  `WithSensitivePaths`. Declared paths are combined with automatically detected
  secret references and remain redacted across hooks, runtime mutations,
  versions, rollback, diffs, generated documentation, and source inspection.

### Fixed

- Make explicit `WithEnv` selection authoritative for environment-specific
  self-config overlays and scope cached self-config results by environment;
  callers now receive detached settings instead of cache-owned pointers.
- Register declarative secret providers created during runtime mutation with
  the Config lifecycle, including providers initialized lazily after startup,
  and close late creations safely when they race with `Config.Close`.
- Fail construction when dynamic watcher setup fails, watch transitive
  composition includes, and atomically adopt changed dependency sets after
  successful reload and extend transactions.
- Validate nil and typed-nil collaborators, option enums, merge paths, hook
  registrations, and secret resolver hooks before source I/O; defensively own
  caller-provided option slices, maps, and inline schemas.
- Preserve secret-derived sensitivity metadata through mutation, refresh,
  source transactions, overrides, version persistence, and rollback. Config
  diff/drift, CLI diff, documentation generation, schema/explain output, and
  detached source-tracker inspection redact those paths by default.

- Make an override restore function a no-op after `Config.Close` so the closed
  snapshot remains immutable and emits no further lifecycle signals.
- Detach `reload`, `extend`, and `override_restored` event payloads completely;
  listeners can no longer reach live configuration through shared slice values.
- Return structured `ConfigError` values (stable `errors.Is` categories) for a
  `Set` rejected by `WithOverride(false)`, version save and rollback admission
  failures, nil diff targets, documentation/export format errors, serializer
  failures, and export or debug-report write failures.
- Classify AWS Secrets Manager not-found responses by the SDK's typed
  `ResourceNotFoundException` and GCP already-exists responses by gRPC status
  code instead of matching error message text, with typed-versus-message
  regression coverage for both providers.
- Reject an explicit secret version against a Vault KV v1 mount instead of
  silently serving the current value.
- Treat an empty `.confii.json` self-configuration file as defaults, matching
  the YAML and TOML behavior.
- Migrate the integration self-configuration fixture from removed
  `default_files` and `deep_merge` fields to canonical `sources` configuration.
- Account for the AWS SDK v1 S3 crypto advisories inherited through the
  official Vault AWS authentication module with scoped OpenVEX statements;
  CI now proves that the vulnerable S3 crypto package is absent from the
  compiled secret-cloud and cloud-example dependency graphs.
- Make parallel secret materialization cancellation race-free by queuing the
  bounded work plan before workers start, and fail composition when an include
  path cannot be canonicalized instead of weakening cycle detection.

### Changed

- Return capability-oriented views from `EnableObservability`, `EnableEvents`,
  and `EnableVersioning` instead of Config-owned mutable collaborators. Metrics
  control remains on Config, event consumers can subscribe without fabricating
  lifecycle events, and version consumers can inspect history without bypassing
  Config's transactional save and rollback operations.
- Make disabling metrics pause every access and lifecycle counter consistently;
  `Config.ResetMetrics` clears retained observations without exposing the
  mutable collector.

- Give `Typed` and `TypedWithContext` identical cached shared-view semantics,
  and add `TypedCopy` plus `TypedCopyWithContext` for explicitly detached typed
  models. Context selection now changes operation control, not value ownership.
- Return a detached, redacted tracker from `Config.SourceTracker` instead of
  exposing the live mutable collaborator.
- Return detached version records from save, lookup, list, and latest-version
  operations so caller mutation cannot corrupt retained version history.

- Adopt Go semantic import versioning for the v2 API. The core module is now
  `github.com/confiify/confii-go/v2`; the independently versioned cloud modules
  are `github.com/confiify/confii-go/loader/cloud/v2` and
  `github.com/confiify/confii-go/secret/cloud/v2`.
- Replace the split legacy/context hook surfaces with one context-aware,
  error-returning contract. Hook operations capture immutable execution plans,
  provider and condition failures propagate to callers, and hook registration
  invalidates the effective typed-model cache. Value-hook matching now occurs
  at its documented pipeline stage, after key hooks, so a key transformation
  can select a value hook in the same operation.
- Make `Config.ToDict`, `Config.Diff`, and `Config.DetectDrift` return errors so
  hook failures can no longer be silently discarded.
- Reject empty, nil, and duplicate declarative provider registrations instead
  of silently accepting ambiguous process-global state.
- Use the Confii-owned `confii` struct tag for configuration mapping. The v2
  typed decoder no longer treats `mapstructure` as a Confii mapping contract;
  independent `validate` tags continue to work unchanged.
- Standardize every paired explicit-context API on the unambiguous
  `OperationWithContext` form. This includes construction, builders, access,
  mutation, snapshots, composition, watchers, events, and Vault/OpenBao
  constructors; the v2 surface no longer mixes `Ctx`, `OperationContext`, and
  `ContextOperation` naming.
- Canonicalize overlapping core source types to `yaml`, `json`, `toml`, `ini`,
  `dotenv`, `environment`, and declarative `environment_files`; explicit CLI
  loaders retain the separate `http` transport. Remove ambiguous v1 aliases,
  make the declared local-file type authoritative, and reject paths whose
  format contradicts that declaration while retaining `.yml` and `.cfg` as
  valid filename extensions. Reject complete JSON documents presented through
  a selected YAML, TOML, INI, or dotenv parser instead of accepting or
  reinterpreting cross-format content.
- Consolidate export serialization, snapshot validation, environment
  selection, and format detection behind one implementation per concern.
  Runtime mutations now pass through the same validation transaction as
  construction, reload, extension, override, and secret refresh.
- Delegate AppRole, Kubernetes, AWS IAM, Azure managed identity, and GCP Vault
  authentication to HashiCorp's official auth packages. Retain explicit
  adapters for externally signed AWS requests and externally supplied Azure or
  GCP identity JWTs, whose acquisition is intentionally owned by the caller.
- Replace the archived mapstructure module and original YAML import with their
  maintained successors. Consolidate explicit and declarative dotenv loading
  on godotenv while preserving Confii's nested keys, scalar coercion, variable
  expansion, and error policies.
- Harden remote loaders with standards-based media type parsing, exact Git
  provider host validation, bounded HTTP response bodies, and injectable HTTP
  clients.
- Replace timestamp-hash version identifiers with monotonic ULIDs, represent
  timestamps as `time.Time`, publish snapshot files through Google's renameio
  implementation, snapshot metadata immutably, and return the canonical typed
  diff model from version comparisons.

### Added

- Add a v2 migration guide covering module paths, struct tags, hooks,
  error-returning snapshots/diffs, provider registration, and typed-hook cache
  behavior.
- Add `Config.GetFloat64Or`, completing the default-returning typed getter set
  already documented by the project.
- Make the existing `Exporter` and `Validator` contracts usable application
  extension points through `WithExporter`, `WithValidator`, and their builder
  equivalents. Custom validators receive isolated candidate data and reject a
  snapshot atomically; custom exporters can add formats or replace built-ins.
- Implement value-safe existence and metadata capabilities for `DictStore`,
  and use `FileTracker.GetChangedFiles` for batched reload decisions.
- Add the dependency-free public `configmap` package for deterministic
  Confii-compatible key-path lookup, existence checks, enumeration, and atomic
  mutation. Core and cloud loaders now share its typed invalid-path,
  nil-map, and path-conflict behavior instead of maintaining separate nested
  map implementations.

## [1.4.1] - 2026-07-30

### Fixed

- Make `consumer-unlink-dev` recover safely when `go get` persisted synthetic
  zero pseudo-versions while filesystem replacements were active. Cleanup can
  now pin all selected Confii modules with `CONFII_VERSION`, or remove only
  stale zero-version requirements before the consumer runs `go mod tidy`.
- Keep branch-coverage enforcement compatible with directories that contain
  external Go tests but no production Go source. Test-only packages continue
  to run in the regular and statement-coverage passes without being sent to a
  source instrumenter that has no production code to instrument.

## [1.4.0] - 2026-07-29

### Added

- Eagerly materialize the final selected configuration before `New` returns:
  required Vault and cloud references now fail startup atomically, repeated
  fields from one remote secret are deduplicated, normal getters stay
  in-memory, reload/extend/runtime mutations preserve the ready-config
  invariant, and `RefreshSecrets` provides an explicit transactional rotation
  path. `WithSecretResolver` also invalidates resolver caches during refresh.

- Add explicit `endpoint` configuration for the opt-in S3 and SSM loaders and
  `path_style` control for S3-compatible services. The matching Go options,
  `WithS3Endpoint`, `WithS3PathStyle`, and `WithSSMEndpoint`, make LocalStack
  and other emulators usable without global AWS SDK endpoint overrides.

- Add `make install-dev` for installing the current CLI checkout with
  commit-aware `dev-<commit>` metadata and a `-dirty` marker for tracked local
  changes. `INSTALL_DIR` supports isolated prerelease testing, and the existing
  `make install` target remains a compatibility alias. Add a guarded
  `make uninstall` goal for the same resolved installation directory and reversible
  `consumer-link-dev` and `consumer-link-dev-cloud` workflows for exercising a
  local checkout from another Go module without conflating CLI installation
  with library dependency selection.

- Add declarative named secret providers, environment-specific defaults, and
  explicit `${secret@provider:key[:json_path][:version]}` routing for mixed
  Vault, AWS, Azure, GCP, file, environment, and custom-provider strategies.
  Existing single-provider configurations and `${secret:key}` references stay
  compatible. Provider factories initialize lazily, introspection reports only
  safe aliases, and declarative resolution now correctly honors JSON paths and
  versions.

- Add `confii env` environment management with current/default/switcher
  introspection, named-file and sectioned environment listing, JSON output,
  validated `set`, and safe `reset`. Default updates preserve unrelated
  YAML/TOML content and file permissions, while an active `env_switcher`
  override is reported instead of being misrepresented as changed.
- Add `Config.AvailableEnvironments()` so applications can obtain the same
  sorted, deduplicated environment inventory as the CLI.

- Add a reusable provider-enabled CLI root and `confii connections test` for
  value-safe, fail-closed preflight of the selected environment's declarative
  sources and secret providers. Context-aware source registration covers Git,
  AWS S3 and SSM, Azure Blob, GCS, and IBM COS without adding their SDKs to the
  core module or standalone CLI.

### Fixed

- Correct Vault browser OIDC interoperability by retaining the nonce from
  Vault's authorization URL and supplying it during the callback exchange.
  Standards-compliant providers such as Keycloak return `state` and `code` at
  the redirect URI rather than echoing the request nonce as a query parameter.

## [1.3.1] - 2026-07-28

### Fixed

- Report the module version embedded by `go install package@version` when the
  CLI was not built through GoReleaser; reserve `dev` for unversioned local
  builds while retaining linker-injected release versions as the priority.
- Make the library and CLI installation scopes explicit and align every
  from-scratch onboarding path on `go mod init`, `go get`, `go install`,
  version verification, and `confii init`. CI now rejects drift among the
  README, documentation homepage, quick start, and `llms.txt`.

## [1.3.0] - 2026-07-28

### Added

- Add an idempotent `confii init [directory]` project bootstrapper with an
  interactive or flag-selected named-file/sectioned layout, complete embedded
  `.confii.yaml`, loadable starter configuration, existing-project and
  ambiguity detection, whole-plan preflight, dry-run, and rollback-safe writes.
- Add declarative `merge_strategy` and `merge_strategy_map` settings so the
  advanced global and per-path merge controls are available through
  `.confii.yaml`, while explicit Go options retain priority.

### Changed

- Decode discovered YAML, JSON, and TOML self-configuration strictly. Unknown
  top-level settings, malformed input, unreadable candidates, and trailing
  YAML/JSON documents now fail startup with a typed configuration error;
  provider-specific fields nested under `sources` and `secrets` remain
  extensible.
- Allow `confii get <key>` to use the environment resolved by `.confii.yaml`,
  while retaining `confii get <environment> <key>` compatibility. Allow
  `confii validate` to use self-config `schema_path`, with `--schema` as the
  explicit override.
- Make initializer next steps layout- and target-aware, including new-module
  guidance, a required `cd` for another target directory, correct minimal-mode
  instructions, and a warning that `--force` never deletes obsolete files.

### Fixed

- Install a declarative `env_prefix` loader after declarative file sources so
  nested variables such as `APP_SERVER__PORT=9090` are a real final override
  layer. Constructor option order no longer loses the auto-loader, while an
  explicitly supplied equivalent environment loader keeps its chosen order.
- Make initializer path validation portable across Unix and Windows, rejecting
  absolute, UNC/rooted, drive-qualified, and traversal paths expressed with
  either separator style while reporting non-directory ancestors consistently.
- Align the self-config example, quick start, CLI reference, installation
  module boundaries, Vault/OpenBao qualification, and example discoverability
  with the tested runtime behavior.

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

- **Re-entrant hook deadlocks.** `GetWithContext`, `ToDictWithContext`, `ExportWithContext`, and
  `TypedWithContext` now snapshot configuration under lock and execute user hooks
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
- **Atomic rollback in `Config.Reload`.** A load, validation, or dry-run failure now
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
- Cloud secret stores: AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, and a Vault-compatible layer with nine implemented auth flows (Token and AppRole live-tested against OpenBao; other flows protocol-tested and provider-configured)
- Merge strategies (replace, merge, append, prepend, intersection, union) with per-path overrides
- Hydra-style config composition via `_include` and `_defaults` directives
- Environment resolution with automatic default + environment-specific merging
- Key, value, condition, and global hooks for value transformation
- Struct tag validation via go-playground/validator and JSON Schema validation
- Full introspection: Explain(), Layers(), Schema(), source tracking, override history
- Config diff, drift detection, versioning with rollback
- File watching with incremental reload (mtime + SHA256)
- Observability: access metrics, event emission
- Documentation generation (markdown, JSON)
- Export to JSON, YAML, TOML
- Self-configuration via `.confii.yaml` auto-discovery
- CLI commands for loading, inspecting, validating, exporting, comparing, and migrating configuration
- Focused runnable examples
- GitHub Actions CI/CD: test matrix, CodeQL, govulncheck, OSSF Scorecard
- Broad unit, integration, race, fuzz, and cross-platform test coverage
