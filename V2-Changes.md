# Confii v2 Branch Change and Verification Dossier

This document is an evidence-oriented account of the work committed on
`feat/v2-api-hardening`. It is intended for an independent technical reviewer,
not as marketing copy. It records why each change was made, the intended
contract, the implementation surfaces, the tests and documentation that support
the claim, later commits that supersede an earlier implementation, and known
limitations or unresolved review items.

The dossier does **not** ask a reviewer to infer correctness from a commit
message. Every behavioral claim below names source, test, or documentation
artifacts that can be inspected independently.

## 1. Audit scope and reproducibility

### 1.1 Exact range

| Property | Value |
| --- | --- |
| Base commit | `4c52568cb51d50e8bca9136b68c22d0c98346840` |
| Base release | `v1.4.1` |
| Audited head | `c179a77e7c0f2f3cb6e6a5dee06766dccb09196b` |
| Audited head tree | `e2f68e5b5dbf95a9fb81930839e6036d3642e9ce` |
| Branch | `feat/v2-api-hardening` |
| Commits in range | 6 |
| Aggregate diff | 350 files, 13,309 insertions, 13,131 deletions |
| SHA-256 of `git diff --binary <base>..<head>` | `7405f2cd4682e88d0d8cfb747e7bf6db24ab8cb3054158579abf5793ba5df333` |

The hash covers the binary patch produced by the Git version used during this
audit. A reviewer should use the commit and tree identifiers as the primary
identity and the patch hash as an additional tamper-evidence check.

This file describes the six commits ending at the audited head. The commit that
adds this dossier is necessarily outside that range and changes documentation
only.

### 1.2 Commands to reconstruct the evidence

```bash
base=4c52568cb51d50e8bca9136b68c22d0c98346840
head=c179a77e7c0f2f3cb6e6a5dee06766dccb09196b

git log --reverse --show-signature "$base..$head"
git diff --stat "$base..$head"
git diff --name-status "$base..$head"
git diff --binary "$base..$head" | shasum -a 256
git rev-parse "$head^{tree}"
```

Per-commit file lists are deliberately not copied into this document because a
copied list can drift. The authoritative list is:

```bash
git diff-tree --no-commit-id --name-status -r <commit>
```

### 1.3 Commit sequence

| Commit | Subject | Files | Additions | Deletions | Relationship |
| --- | --- | ---: | ---: | ---: | --- |
| `ef785cf807b33fbbc52b0a63073bb29b31092316` | `feat!: harden the v2 configuration lifecycle` | 324 | 8,984 | 11,232 | Foundational v2 break and contract migration |
| `dfea81c3af3b3864243d3ad1375bed38181443f3` | `Harden v2 configuration extension APIs` | 44 | 1,022 | 306 | Public extension points and shared map-path contract |
| `2a9bce09aed745ffdfae7525032a3ff6c57dd497` | `Harden v2 providers and configuration lifecycle` | 62 | 1,962 | 1,272 | Maintained dependencies, official Vault auth, parser and remote-loader hardening |
| `c1e5d0151b345fcb9f3669da8ab6f253a9c6092b` | `Consolidate source lifecycle transactions` | 10 | 582 | 566 | Replaces separate Reload/Extend transaction runners |
| `2847b91fc6af34c9c4ad78f7ba0ac7d1637c18fe` | `Make runtime mutations transactional` | 4 | 607 | 306 | Applies private-candidate publication to Set/Override |
| `c179a77e7c0f2f3cb6e6a5dee06766dccb09196b` | `Harden lifecycle publication and error contracts` | 36 | 987 | 284 | Shared post-commit delivery, transactional refresh/rollback, stable errors |

All six commits have valid cryptographic commit signatures as verified by
`git verify-commit`. The first five contain
`Signed-off-by: confiify <confii.connect@gmail.com>`. The sixth does not contain
a DCO trailer; this is recorded as an open process finding in section 11 and
must be resolved before a DCO-protected pull request can pass without an
exception or history correction.

## 2. Cross-commit architectural goal

The branch addresses a single central problem: v1 accumulated overlapping
configuration controls, compatibility aliases, read-time transformations, and
mutation paths with different publication behavior. Those properties made it
possible for typed and untyped reads to disagree, for a long-running operation
to publish after lifecycle state changed, for two concurrent writers to
overwrite each other, and for configuration syntax to be interpreted through
fallback heuristics.

The v2 target is one explicit lifecycle:

```text
discover self-config
  -> construct ordered sources
  -> load and compose
  -> select environment
  -> run immutable transformation plan
  -> resolve required secrets
  -> validate schema, typed model, and custom validators
  -> publish one ready snapshot
  -> deliver callbacks, metrics, and events after unlocking
```

The corresponding invariants are:

1. A published snapshot is fully materialized and validated.
2. `Get`, `Typed`, `ToDict`, export, introspection, and normal reads observe the
   same values; reads do not rerun transformation hooks or provider I/O.
3. Candidate-building work does not partially modify live configuration.
4. Publication rechecks context cancellation, `Close`, and `Freeze`.
5. Operations derived from current state also compare a revision and retry
   rather than overwrite a newer commit.
6. Callbacks and synchronous event listeners run after the configuration lock
   is released and receive detached data.
7. Declarative names and file formats are explicit; parse failure does not
   silently reinterpret content as a different format.
8. Errors expose stable categories through `errors.Is`, `errors.As`, and
   `ConfigError.Code`, not through message prefixes.

The final form of these invariants is implemented primarily by
`config_materialize.go`, `config_source_transaction.go`,
`config_runtime_mutation.go`, `config_publication.go`, and the lifecycle methods
that call them. `docs/ARCHITECTURE.md` documents the maintained invariant for
future contributors.

## 3. Commit `ef785cf`: foundational v2 lifecycle and API break

### 3.1 Reason and goal

This commit intentionally uses a breaking-change marker. The goal was to remove
ambiguous v1 compatibility surfaces before they became permanent public
contracts, and to make construction produce a ready, consistent snapshot.
Because this touched module identity, contexts, hooks, self-configuration,
sources, secrets, CLI behavior, examples, validation, releases, and tests, it is
the largest commit in the series.

### 3.2 Go v2 module identity

The root module became `github.com/confiify/confii-go/v2`. The independently
versioned optional modules became:

- `github.com/confiify/confii-go/loader/cloud/v2`
- `github.com/confiify/confii-go/secret/cloud/v2`

Imports, examples, scripts, release configuration, API-compatibility logic, and
documentation were updated to use Go semantic import versioning. The nested
module suffix belongs after the nested module path. The first v2 release has no
released v2 API baseline; the compatibility check therefore reports that fact
instead of incorrectly comparing v2 against v1.

Evidence: root and nested `go.mod` files, `.goreleaser.yaml`, release scripts,
`docs/v2-migration.md`, and module-boundary tests.

### 3.3 Context API and bounded implicit operation contexts

The user-facing constructor split is now:

```go
confii.New[T](options...)
confii.NewWithContext[T](ctx, options...)
```

`New` supplies a background context and applies Confii's startup timeout.
`NewWithContext` preserves caller cancellation and an existing caller deadline;
the fallback timeout is added only when the caller has no deadline. The builder
uses the equivalent `Build` / `BuildWithContext` split.

Every paired API follows `Operation` / `OperationWithContext`. Ambiguous names
such as `SetContext`, suffixes such as `Ctx`, and mixed word order were removed.
The context-free forms create bounded implicit operation contexts, so a user is
not forced to add context boilerplate to receive a finite default. The explicit
forms remain available for request, shutdown, trace, or deployment deadlines.

Defaults and controls introduced or canonicalized here:

- startup timeout: 60 seconds;
- runtime operation timeout: 30 seconds;
- secret resolution concurrency: 4;
- zero timeout: do not add a Confii deadline;
- negative timeout or invalid concurrency: rejected during construction;
- caller deadline: never extended or replaced;
- cancellation: control flow that is not swallowed by warn/ignore source error
  policies.

`Config.Close` owns the runtime lifecycle: it stops owned watchers and closes
owned provider resources. The last published snapshot remains readable after
close, while mutations return `ErrConfigClosed`.

Evidence: `config.go`, `config_options.go`, `config_operation_context.go`,
`context.go`, `builder.go`, `config_context_test.go`,
`config_context_lifecycle_test.go`, `context_api_naming_test.go`, and
`docs/context.md`.

The context naming pass also reaches collaborating public APIs rather than
stopping at `Config`:

- the composer provides `ComposeWithContext` and
  `ComposeWithDependenciesWithContext`, checks cancellation around recursive
  include reads/parsing, rejects nil context, and does not mutate the input on
  failure;
- the watcher provides `ReloadFuncWithContext` and `NewWithContext`, using its
  lifecycle context for reload callbacks;
- the event emitter provides `OnWithContext` and `OffWithContext`, and
  operation events propagate the originating context;
- LDAP `PasswordProvider` now accepts `context.Context`, allowing cancellation
  of a lazily obtained password;
- Vault/OpenBao constructors, composition, watch, access, mutation, version,
  event, and builder APIs use the same suffix convention.

### 3.4 Confii-owned typed mapping contract

Typed decoding now uses the project-owned `confii` struct tag rather than
treating another library's `mapstructure` tag as Confii's public API:

```go
type AppConfig struct {
    LogLevel string `confii:"log_level" validate:"required"`
}
```

The mapping controls `-`, `squash`, and `remain` are supported. The
`validate` tag is independent and remains active. The reason was ownership and
clarity: switching an internal decoder should not change the tag users regard
as Confii's contract.

Evidence: typed decode and validation code, mapping tests, examples,
`docs/access.md`, and `docs/v2-migration.md`.

### 3.5 Hooks moved from read-time behavior to snapshot materialization

V1 had split hook and context-hook types, allowed registration after
construction, and could rerun transformations during different access paths.
That made typed fields bypass or duplicate key-based behavior and allowed reads
to disagree.

V2 uses one context-aware, error-returning hook function:

```go
func(context.Context, string, any) (any, error)
```

Conditions also accept context and return `(bool, error)`. Separate `FuncCtx`,
`HookCtx`, `ProcessCtx`, and `Register*HookCtx` surfaces were removed. Hooks are
registered through constructor options or the builder and compiled into an
immutable execution plan. The Config does not expose a mutable processor for
post-construction registration.

The execution order is key hooks, value hooks matched against the transformed
value, condition hooks, then global hooks. Built-in environment expansion,
type conversion, and secret resolution participate in materialization rather
than being reapplied by ordinary getters. Errors and cancellation fail the
candidate instead of being silently converted to an unchanged value.

This directly resolves the typed-access concern: `Typed()` reads the same
already-transformed snapshot as `Get()`, so direct field access does not bypass
hooks. `Set`, `Override`, `Extend`, `Reload`, and `RefreshSecrets` rematerialize
a candidate through the same plan before publication.

Evidence: `hook/`, `config_materialize.go`, `config_hooks_test.go`,
`hook/processor_test.go`, `docs/hooks.md`, and `docs/access.md`.

Typed pointer behavior is explicit. `Typed()` may return the same cached `*T`
until another snapshot is published. Mutating that pointer changes the cached
typed view but does not write back to `Get` or `ToDict`; configuration changes
must use `Set` or another mutation API. `TypedWithContext()` decodes a fresh,
independent `*T` on every call. Neither form reruns hooks or remote secret
resolution.

### 3.6 Canonical self-configuration discovery and overlays

The project contract is one self-configuration format at a time. Supported
base candidates are hidden or visible YAML/YML, JSON, and TOML files:

```text
.confii.yaml  .confii.yml  .confii.json  .confii.toml
 confii.yaml   confii.yml   confii.json   confii.toml
```

The hidden family is used when it is the only family present; the visible
family is a fallback when it is the only family present. Hidden and visible
files are **not** silently prioritized when both exist: mixed families or
multiple base formats are reported as ambiguity errors.

After decoding the base, `env_switcher` selects an environment, falling back to
`default_environment`. Confii optionally loads exactly one matching overlay:

```text
.confii.<environment>.<same-extension>
confii.<environment>.<same-extension>
```

The overlay must match the base's hidden/visible family and extension. It is
recursively merged over the base. Unsafe or ambiguous environment names are
rejected. `confii init` creates only the base and documents how an operator may
add an environment overlay.

Evidence: `selfconfig/reader.go`, `selfconfig/reader_test.go`,
`selfconfig/default.confii.yaml`, initializer tests, and
`docs/configuration.md`.

### 3.7 Canonical self-configuration schema

Several overlapping controls were removed:

- `sources` is the only declarative source list; `default_files` was removed.
- `env_prefix` is the only process-environment prefix; the compatibility alias
  `default_prefix` was removed.
- merge behavior uses `merge.default` and `merge.paths`; the parallel
  `deep_merge` boolean and old aliases were removed.
- the merge enum is `replace`, `shallow_merge`, `deep_merge`, `append`,
  `prepend`, `intersection`, or `union`.
- `WithMergeStrategy` replaces `WithMergeStrategyOption`; `WithDeepMerge`,
  `EnableDeepMerge`, and `DisableDeepMerge` were removed.
- secrets require `secrets.providers`; each provider entry requires an
  explicit `type`.
- a provider alias identifies an instance and no longer implicitly selects the
  provider implementation.
- unqualified `${secret:key}` requires an explicit default provider or matching
  environment default; explicit `${secret@alias:key...}` remains the mixed
  provider form.

Explicit constructor options still have higher priority than self-config, and
self-config has higher priority than built-in defaults.

Evidence: `selfconfig/reader.go`, `selfconfig/default.confii.yaml`,
`config_self.go`, `config_self_merge_test.go`, `config_secret_self.go`, and
secret self-config tests.

### 3.8 Canonical source types and cross-format fail-closed behavior

Declarative source names are now:

- `yaml`
- `json`
- `toml`
- `ini`
- `dotenv`
- `environment`
- `environment_files`

Ambiguous aliases such as `env`, `envfile`, `env-vars`, `cfg`, `yml`, and the
hyphenated environment-files form were removed from the declarative and CLI
type vocabulary. This does not prohibit conventional file extensions:
`type: yaml` accepts `.yaml` or `.yml`, and `type: ini` accepts `.ini` or
`.cfg`.

The declared type is authoritative. A contradictory path is rejected. A
complete JSON document presented through YAML, TOML, INI, or dotenv is rejected
rather than accepted as a permissive subset or retried using a different
parser. The goal is to prevent configuration that appears valid under the
wrong grammar and later changes behavior when moved between environments.

Evidence: `loader/format.go`, file loaders, declarative source parsing,
cross-format tests, CLI loader parsing, and `docs/sources.md`.

### 3.9 Environment-selection semantics

The former one-key `default:` compatibility exception was removed. Interpretation
now depends on the selected `environment_strategy`:

- `sectioned` interprets environment sections explicitly;
- `named_files` composes default and selected environment files;
- `flat` treats `default` as an ordinary application key;
- `auto` follows documented source evidence rather than a hidden one-key
  special case;
- `hybrid` remains available only with explicit conflict handling.

This prevents the shape of user data from silently selecting a different
configuration model.

Evidence: `envhandler/handler.go`, environment strategy tests,
`docs/environment.md`, and `docs/sources.md`.

### 3.10 Secret provider contract and eager resolution

The compatibility pair of secret-store interfaces was replaced by one
request-aware contract. A request carries key, field, and version, and every
declarative provider implements `ReadSecret(context.Context, SecretRequest)`.
Factories are context-aware and registrations reject empty names, nil or
typed-nil factories, and duplicate normalized names. Registration failures are
intentional startup panics because import order must not select an
implementation silently.

Global soft-failure modes were removed. Required references fail
materialization. Multi-store fallback continues only for the typed
not-found condition; authentication, authorization, network, parse, and other
backend errors stop fallback. Optionality must be explicit rather than enabled
by a global “leave unresolved” switch.

After ordered sources are composed and the environment is selected, all
remaining secret references are grouped and resolved before publication.
Ordinary reads therefore do not require an explicit resolution call and do not
perform provider I/O. `RefreshSecrets` is the explicit rotation operation.

Evidence: `config_secret_self.go`, `secret/resolver.go`, `secret/multi.go`,
provider adapters and tests, `config_materialize.go`, `docs/secrets.md`, and
`docs/v2-migration.md`.

### 3.11 Vault/OpenBao naming and field extraction

The concrete type became vendor-neutral `VaultStore`. `NewVault`,
`NewHashiCorpVault`, and `NewOpenBao`, plus their explicit-context forms, return
that neutral implementation. OpenBao is no longer represented as a HashiCorp-
named object merely for compatibility. Provider-specific field shortcuts were
reconciled with the generic request field so declarative stores share one field
extraction path. The provider-neutral `WithField` secret option exposes the same
field selection to direct store consumers.

Evidence: `secret/cloud/vault.go`, Vault/OpenBao tests, and
`docs/secrets.md`.

### 3.12 CLI and initializer behavior

`confii get` accepts exactly one positional key. An environment override is
explicitly supplied by `--environment` / `-e`; otherwise the project-selected
environment is used. The positional `get <environment> <key>` compatibility
form was removed.

`confii migrate` requires two arguments:

```text
confii migrate <source-type> <config-file>
```

Supported migration types are `dotenv`, `yaml`, `dynaconf`, `hydra`, and
`omegaconf`. A YAML parse failure is not reinterpreted as dotenv. Dynaconf's
document format is selected from its extension; Hydra and OmegaConf migration
consume materialized YAML and do not execute the source tool's resolver model.

`confii init` now:

- detects an existing valid project and remains idempotent;
- rejects ambiguous self-config candidates and symlink/non-regular targets;
- asks for YAML, JSON, or TOML when interactive;
- asks for named files, one sectioned file, or self-config-only layout;
- supports non-interactive flags, dry-run, configurable environments,
  directory, switcher, and default environment;
- preflights the whole write plan before modifying files;
- uses exclusive creation unless `--force` is selected;
- does not let `--force` create a competing self-config format;
- rolls back files written by an incomplete plan and reports cleanup failure;
- generates the same complete embedded schema understood by the library.

Evidence: `confii/cmd/get.go`, `confii/cmd/debug.go`, `confii/cmd/init.go`,
`confii/cmd/init_scaffold.go`, their tests, and `docs/cli.md`.

### 3.13 Getter and public utility completion

`Config.GetFloat64Or` was added to complete the default-returning typed getter
family. Getter documentation now distinguishes lookup failure from conversion
failure. `GetInt` rejects overflowing `int64` values and fractional, non-finite,
or overflowing floats instead of narrowing silently. `GetFloat64` documents
normal Go precision loss when an integer cannot be represented exactly. `Has`
and `Get` follow the same system-environment fallback path, and `Keys(prefix)`
returns fully qualified paths that can be passed directly to `Get`.

The public `loader.Format`, `FormatFromExtension`,
`FormatFromContentType`, and `ParseContent` functions establish one format
contract for HTTP and optional cloud object loaders. Unknown HTTP metadata uses
deterministic JSON-then-YAML detection; an explicitly selected format remains
authoritative and does not use that fallback.

Evidence: `config_access.go`, access tests, `loader/format.go`,
`loader/http.go`, and their tests.

### 3.14 Error surface in the foundational commit

The branch began moving callers from message-prefix compatibility toward
structured errors and made `ToDict`, `Diff`, and `DetectDrift` return errors so
cancellation or transformation failures could not be discarded. The final
`ConfigError` representation was completed in `c179a77`; section 8 describes
the final contract rather than this intermediate form.

### 3.15 Documentation, examples, and mechanical migration

The commit updated the README, MkDocs pages, CLI references, examples, tests,
release scripts, workflow inputs, module paths, build-tag imports, and API
compatibility checks. It also replaced historical/conversational implementation
comments with API and invariant documentation. Large deletion counts include
removed aliases, replaced tests, import-path migration, and duplicated behavior;
they should not be interpreted as 11,232 lines of feature removal.

`docs/v2-migration.md` was added as the consumer-focused migration guide.
`docs/context.md` was added for the context and resource-lifecycle contract.

### 3.16 Important supersession

This commit introduced separate private-candidate paths for Reload and Extend.
Those paths were deliberately consolidated by `c1e5d0`. Review the final
`config_source_transaction.go`, not only the intermediate files in this commit.

## 4. Commit `dfea81c`: extension APIs and shared configuration paths

### 4.1 Reason and goal

The initial v2 pass exposed two issues: existing `Exporter` and `Validator`
interfaces were not usable as deterministic application extension points, and
several packages maintained similar nested-map traversal code. The goal was to
make useful utilities public where consumers could benefit, remove true
duplication, and ensure extensions participate in the same publication
transaction as built-ins.

### 4.2 Public `configmap` package

The dependency-free `configmap` package provides:

- `Get` for a dot-separated path;
- `Has` without conflating a present nil value with absence;
- `Set` with atomic failure semantics;
- `Keys` for sorted, deterministic leaf paths;
- `ErrInvalidPath`, `ErrNilMap`, `ErrPathConflict`, and `PathError` for typed
  diagnosis.

`Keys` is cycle-safe. `Set` does not leave intermediate maps behind when a path
is invalid or crosses a non-map value. The package does not add synchronization;
callers own concurrent access. Core and optional cloud code use this contract,
replacing duplicate path implementations. Internal helpers with no supported
future contract, including `FlatKeysWithPrefix` and `Unflatten`, were removed
rather than exposed speculatively.

Evidence: `configmap/doc.go`, `configmap/path.go`, and
`configmap/path_test.go`.

### 4.3 Exporter registration

`WithExporter` and the builder equivalent make custom serializers operational.
Registrations are validated before loading sources:

- format names must already be in canonical lowercase form;
- empty names, surrounding whitespace, and noncanonical names are rejected;
- nil and typed-nil implementations are rejected;
- a later registration for the same format replaces the earlier one;
- custom formats can supplement or intentionally replace JSON, YAML, or TOML.

Export serialization is centralized. Because a materialized snapshot can
contain resolved secrets, file export uses mode `0600`, replacing the former
`0644` behavior.

Evidence: `config_extensions.go`, `config_extensions_test.go`, export code, and
`docs/export.md`.

### 4.4 Validator registration

`WithValidator` and the builder equivalent add custom validation to every
publishing lifecycle. A validator receives a deep copy so it cannot mutate the
candidate that will be published. Validation order is schema validation,
typed-model validation, then custom validators. A failure rejects the candidate
atomically during construction, reload, extension, Set, Override, refresh, or
rollback.

Strict typed validation remains the default when typed validation is enabled.
Selecting non-strict behavior affects typed-tag warnings; JSON Schema and
custom validator failures remain fatal.

Evidence: `config_extensions.go`, `config_extensions_test.go`,
`config_validation.go`, and `docs/validation.md`.

### 4.5 Additional consolidation and capabilities

- `DictStore` gained optional value-safe existence and metadata capabilities;
  callers can inspect availability/version metadata without fetching or
  exposing a secret value.
- `FileTracker.GetChangedFiles` became the shared batched reload-decision path.
- export serialization, validation dispatch, environment selection, and format
  detection were consolidated so each concern has one authoritative
  implementation.
- `.graphifyignore` excludes generated `graphify-out/**` material from future
  graph extraction; generated analysis is not production source.
- architecture, export, validation, reload, secret, configuration, and FAQ
  pages were updated for the new contracts.

## 5. Commit `2a9bce0`: provider, dependency, parsing, and remote-I/O hardening

### 5.1 Reason and goal

An audit found hand-built authentication flows and duplicated parsers where
maintained, purpose-specific libraries offered stronger compatibility. The goal
was not to delete custom code first. Official or maintained implementations
were integrated, parity and failure behavior were tested, and only then were
superseded paths retired. Confii-specific policy—nested keys, scalar coercion,
cross-format rejection, error wrapping, and lifecycle publication—remains in
Confii.

### 5.2 Official Vault authentication packages

The ordinary AppRole, Kubernetes, AWS IAM, Azure managed identity, and GCP auth
flows now delegate credential discovery and login construction to HashiCorp's
official Vault auth packages:

- AppRole `v0.12.0`;
- AWS `v0.12.0`;
- Azure `v0.11.0`;
- GCP `v0.12.0`;
- Kubernetes `v0.12.0`.

Confii still validates mutually exclusive inputs, maps declarative settings,
checks the returned token, and wraps errors without leaking credentials.

Provider-specific behavior:

- AppRole requires exactly one of inline secret ID, secret-ID file, or
  secret-ID environment variable; an optional wrapping token is supported;
  rotating file/environment input is read for each authentication.
- Kubernetes accepts at most one inline JWT, environment source, or token
  path; no explicit source delegates to the standard service-account path.
- AWS IAM uses the standard AWS credential chain and supports role, region,
  server-ID header, and mount selection.
- Azure obtains managed identity and instance metadata from Azure IMDS.
- GCP supports `gce` and `iam`; IAM requires the service-account email.

Explicit caller-owned credential adapters remain because they are distinct
use cases, not duplicates:

- `AWSIAMSignedRequestAuth` accepts an already signed request;
- `AzureJWTAuth` accepts an externally brokered identity JWT;
- generic `JWTAuth` with the GCP mount supports an externally supplied GCP JWT.

Token, LDAP, JWT, and browser OIDC continue to use the Vault API directly
because there is no equivalent official helper package that removes Confii's
required behavior.

Evidence: `secret/cloud/vault_auth_official.go`,
`secret/cloud/vault_auth_explicit.go`, `secret/cloud/vault_auth_official_test.go`,
the behavior tests, self-config forwarding tests, and `docs/secrets.md`.

### 5.3 Maintained decoding and parsing dependencies

The root moved to maintained implementations:

- `github.com/go-viper/mapstructure/v2 v2.5.0` for typed mapping internals;
- `go.yaml.in/yaml/v3 v3.0.5` for YAML;
- `github.com/joho/godotenv v1.5.1` for dotenv grammar;
- `github.com/oklog/ulid/v2 v2.1.2` for version identifiers;
- `github.com/google/renameio/v2 v2.0.2` for atomic file replacement.

BurntSushi TOML was retained deliberately: replacing a mature decoder solely
because another package has a larger brand would add migration risk without a
demonstrated contract or security gain.

`internal/configdecode` now owns strict map-only document decoding, single-
document enforcement, key normalization, and cross-format guards.
`internal/dotenvparse` uses godotenv's accepted grammar, then applies Confii's
nested path mapping, scalar coercion, expansion, and error policy. Explicit and
declarative dotenv sources therefore no longer implement similar parsing
independently.

Evidence: `internal/configdecode/`, `internal/dotenvparse/`, loader tests, and
the root and nested module files.

### 5.4 Remote loader hardening

- HTTP source responses are bounded to 8 MiB by default.
- `WithMaxResponseBytes` permits an explicit bound.
- `WithHTTPClient` accepts an injected client and copies it to avoid later
  caller mutation changing loader behavior.
- media types are parsed using standards-aware parsing rather than string
  fragments.
- Git provider URLs require exact allowed hosts, rejecting lookalikes,
  insecure schemes, traversal, and malformed repository paths.
- cloud loader scalar extraction uses a shared helper rather than provider-
  specific copies.

Evidence: `loader/http.go`, loader options and tests,
`loader/cloud/internal/cloudutil`, and Git loader tests.

### 5.5 Version persistence redesign

Version records now use monotonic ULIDs and `time.Time`. The duplicate floating
timestamp and presentation-oriented `DateTime` field were removed.
Configuration and metadata are deep-copied at boundaries. Snapshot files are
written with mode `0600` and atomically published through renameio.
`DiffVersions` returns `[]diff.ConfigDiff` instead of untyped maps. Ordering is
deterministic even when timestamps tie, and persisted filenames are required to
be canonical ULIDs.

This is intentionally not storage-compatible with v1 snapshots. An upgraded
application must establish a new v2 baseline.

Evidence: `observe/version.go`, version tests, `docs/versioning.md`, and
`docs/v2-migration.md`.

### 5.6 Duplicate hook traversal retirement

`config_hooks.go` was deleted after the materialization implementation became
the authoritative traversal. This removal avoids two hook engines drifting in
order, error handling, or typed behavior.

## 6. Commit `c1e5d01`: one source transaction runner

### 6.1 Reason and goal

Reload and Extend had become correct but structurally similar transaction
runners. Keeping both would make later lifecycle fixes easy to apply to one and
miss in the other. The goal was one shared runner with operation-specific
candidate preparation and operation-specific observability.

### 6.2 Final source transaction algorithm

`config_source_transaction.go` introduces `runSourceTransaction` and a private
candidate snapshot:

1. reject nil or canceled context;
2. under a read lock, reject closed/frozen state, record the revision, copy the
   old published view, and snapshot all source-derived state;
3. release the lock and let Reload or Extend perform loader/provider I/O,
   composition, materialization, and validation on the candidate;
4. for a deliberate no-op or dry run, return without publishing or signaling;
5. reacquire the write lock and recheck cancellation, close, freeze, and the
   captured revision;
6. on a revision conflict, discard the candidate and retry from the new state;
7. publish all source-derived fields as one unit and increment the revision;
8. capture detached callback/observability state, unlock, then deliver exactly
   one successful lifecycle signal.

The copied transaction state includes unresolved, selected, and merged maps;
ordered loaders and layers; loader dependencies; source and file trackers;
source plan; and typed-cache generation. Long I/O never holds the live
configuration lock, so readers continue to observe the previous complete
snapshot.

Reload and Extend retain separate preparation logic because their operations
are not identical. The shared code owns only admission, isolation, publication,
retry, and signals.

### 6.3 Regression evidence

`config_lifecycle_regression_test.go` covers, among other paths:

- Extend no-op does not publish or emit lifecycle signals;
- lifecycle listeners may read configuration without deadlocking;
- a revision conflict retries without duplicate signals;
- concurrent Freeze/Close/cancellation prevents publication;
- a missing raw snapshot is reconstructed from the materialized base where
  required;
- source and file tracking remain consistent;
- failed candidates leave live state unchanged.

Architecture, context, and reload documentation were updated to describe the
shared algorithm. This commit supersedes the separate Reload/Extend transaction
runners from `ef785cf`.

## 7. Commit `2847b91`: transactional Set and Override

### 7.1 Reason and goal

After source operations used private candidates, runtime Set and Override still
needed the same guarantees. Their old in-lock/in-place paths could expose
partial mutation, deadlock if a custom validator called back into Config, or
publish a stale candidate after another writer advanced state.

### 7.2 Runtime mutation algorithm

`config_runtime_mutation.go` captures a runtime candidate containing raw,
effective, merged, tracking, and override-stack state. Set and Override:

1. admit the operation and capture a revision;
2. apply the requested mutation only to the candidate;
3. materialize and validate outside the live lock;
4. recheck cancellation, close, freeze, and revision before publication;
5. retry when a concurrent publication supersedes the candidate;
6. publish atomically and deliver success signals after unlock.

A validation failure against an already stale revision is retried rather than
incorrectly rejecting the user's operation based on obsolete state. If
cancellation occurs while validation is running, cancellation is authoritative.
Failed candidates do not modify source attribution, override frames, typed
cache, metrics, callbacks, or events.

Override restore/replay preserves nested-frame semantics and source metadata.
The restore function is idempotent, and rejected restores do not partially pop
or replay the stack.

### 7.3 Regression evidence and documentation timing

`config_runtime_candidate_test.go` covers rejected publication, validator
reentry, concurrent revision advancement, stale validation failure, cancellation
precedence, retry cancellation, and lifecycle changes both after
materialization and immediately before publication.

This commit changed four root files and did not independently change user
documentation. The general transactional invariant already existed in the
architecture material and was made explicit for all mutation classes in the
following `c179a77` documentation changes. This timing is disclosed so a
reviewer does not infer a per-commit documentation update that did not occur.

## 8. Commit `c179a77`: publication, refresh, rollback, and errors

### 8.1 Reason and goal

The final audit found repeated post-commit callback/metric/event sequences,
RefreshSecrets and rollback paths that needed the same lifecycle discipline,
and a legacy `ConfigError` representation with two cause fields and textual
compatibility behavior. The goal was one post-publication contract, full
transaction coverage, and a stable machine-readable error surface.

### 8.2 Shared post-commit delivery

`config_publication.go` introduces `committedChange` and the shared
`captureCommittedChange` / `deliverCommittedChange` path. While the lock is
held, it captures deep-copied before/after snapshots, copies callback slices,
and captures observer/emitter references. After unlock, it:

1. computes the per-key diff;
2. invokes ordinary callbacks;
3. invokes context-aware callbacks with the operation context;
4. records an optional operation-specific metric;
5. emits the operation-specific event;
6. records the generic change metric;
7. emits the generic `change` event with detached complete old/new maps.

Reload, Extend, Set, Override, override restore, RefreshSecrets, and rollback
use this path. Listeners may safely read or mutate the Config because delivery
does not hold the Config lock. A listener cannot mutate live state through an
event payload.

The generic change event intentionally carries full old/new snapshots rather
than the old Set-specific key/value shape, making its contract consistent
across operation types. Operation-specific events retain operation-specific
payloads.

Override restoration has no caller-supplied context because the returned
restore closure has no context parameter. Context callbacks for that operation
therefore receive `context.Background()`; this is a documented design boundary,
not accidental propagation.

### 8.3 Transactional secret refresh

`RefreshSecretsWithContext` now:

- rejects nil/canceled context and closed/frozen state before clearing caches;
- clears managed resolver/provider cache once per operation;
- rebuilds and validates a private candidate from the retained unresolved
  snapshot;
- rechecks lifecycle and revision before publication;
- retries a revision conflict without clearing caches again;
- emits callbacks, metrics, and events only for the committed attempt;
- preserves the last ready snapshot on provider or validation failure.

A zero-value/no-reference configuration is a no-op. Under continuous concurrent
writes, retries are bounded by the effective operation context; a caller that
explicitly disables or omits its own deadline assumes responsibility for that
bound.

### 8.4 Transactional version rollback

Rollback loads a stored materialized snapshot, deep-copies it, validates it
against the current schema/typed/custom validators, and publishes it only after
context and lifecycle checks. It does not call secret providers during
rollback, because a version is a historical materialized state.

Every restored leaf is attributed to source `version:<version-id>` with loader
type `version`. Unresolved and merged views, trackers, and typed cache are kept
consistent. The operation invalidates relevant caches and emits the same
post-commit callback/metric/event contract.

The version target does not depend on current configuration state, so the
write lock is its linearization point and no optimistic revision retry is
needed. After rollback, `RefreshSecrets` has no unresolved references to
re-resolve; a non-incremental Reload is the explicit route back to source-backed
state.

Evidence: `config_rollback_transaction_test.go`, version tests, and
`docs/versioning.md`.

### 8.5 Final structured error contract

`ConfigError` now has one concrete wrapped cause in `Err` and one stable
`ConfigErrorCode`. The legacy `Cause` field and multi-error unwrap were removed.
`Unwrap` returns the concrete cause. `Is` recognizes both the category's
sentinel and the wrapped cause, so normal `errors.Is` / `errors.As` traversal
works for context errors, filesystem/SDK failures, application validator
errors, and Confii categories.

Stable categories are:

| Code | Sentinel |
| --- | --- |
| `config_load` | `ErrConfigLoad` |
| `config_format` | `ErrConfigFormat` |
| `config_validation` | `ErrConfigValidation` |
| `config_not_found` | `ErrConfigNotFound` |
| `config_merge` | `ErrConfigMerge` |
| `config_frozen` | `ErrConfigFrozen` |
| `config_closed` | `ErrConfigClosed` |
| `config_access` | `ErrConfigAccess` |
| `config_invalid` | `ErrConfigInvalid` |

Messages are operator-facing and are not compatibility identifiers. Structured
context remains bounded and must not contain secrets. `config_merge` is part of
the stable category vocabulary even if a reviewer finds fewer direct
construction sites than other categories; the sentinel remains the merge
contract and should be tested when new merge errors are added.

Evidence: `errors.go`, `errors_test.go`, migrated production literals, and
`docs/errors.md`.

### 8.6 Regression evidence

Tests added or expanded in this commit cover:

- refresh admission before cache invalidation;
- refresh cache bypass, revision retry, validation rollback, close/freeze, and
  cancellation;
- one set of refresh signals and metrics for the committed attempt;
- rollback validation, lifecycle admission, source attribution, and signals;
- concurrent Freeze/Close during rollback validation;
- override-restore context callback delivery;
- every ConfigError category and wrapped-cause branch;
- detached generic change payloads and listener reentry.

README, architecture, observability, secrets, versioning, MkDocs navigation,
and the new error guide were updated.

## 9. Public API and behavior traceability

| Requirement or concern | Final implementation | Principal tests | User documentation |
| --- | --- | --- | --- |
| Context optional but bounded | `config.go`, `config_operation_context.go`, `config_options.go` | `config_context_test.go`, `context_api_naming_test.go` | `docs/context.md`, `docs/v2-migration.md` |
| Typed access must honor hooks | `config_materialize.go`, `hook/processor.go` | `config_hooks_test.go` | `docs/hooks.md`, `docs/access.md` |
| Project-owned mapping tag | typed decoder using `confii` | mapping and validation tests | `docs/access.md`, migration guide |
| One self-config family/format | `selfconfig/reader.go` | `selfconfig/reader_test.go`, init tests | `docs/configuration.md` |
| Environment self-config overlay | `selfconfig/reader.go` recursive overlay | `TestReadLayersMatchingEnvironmentSelfConfig` and related cases | self-config template, configuration guide |
| Canonical sources and cross-type rejection | `config_self.go`, `loader/format.go`, `internal/configdecode` | loader/declarative cross-format tests | `docs/sources.md`, migration guide |
| Mixed secret providers | `config_secret_self.go` request router | named-provider and forwarding tests | `docs/secrets.md` |
| Eager secrets and explicit rotation | `config_materialize.go`, refresh implementation | materialization/refresh tests | `docs/secrets.md` |
| Official Vault identity helpers | `secret/cloud/vault_auth_official.go` | official request-parity and behavior tests | secrets and migration guides |
| Custom export and validation | `config_extensions.go` | `config_extensions_test.go` | export and validation guides |
| Shared nested-map behavior | `configmap/` | `configmap/path_test.go` | package godoc, architecture guide |
| Atomic Reload/Extend | `config_source_transaction.go` | source transaction tests | architecture, context, reload guides |
| Atomic Set/Override | `config_runtime_mutation.go` | runtime candidate tests | architecture and context guides |
| Shared after-commit signals | `config_publication.go` | lifecycle, refresh, rollback tests | architecture and observability guides |
| Stable structured errors | `errors.go` | `errors_test.go` | `docs/errors.md` |
| Atomic version persistence | `observe/version.go` | version and concurrency tests | `docs/versioning.md` |
| Safe project initialization | `confii/cmd/init*.go` | initializer and rollback tests | CLI and quick-start guides |

## 10. Verification evidence captured for the audited head

The following repository gates were run against the final audited tree during
the branch work:

- `make ci` — passed;
- `make lint` — passed with zero reported issues;
- `make docs-check` — passed;
- `go test -race ./ -count=1` — passed for the root package;
- non-example statement coverage — approximately 92.45%, above the 90% gate;
- root statement coverage — approximately 97.2%;
- aggregate branch coverage — approximately 84.99%, above the 80% gate;
- API compatibility — correctly reported that no released same-major v2
  baseline exists yet;
- Graphify extraction — 3,473 nodes, 8,703 edges, and 219 communities after
  synchronization at the audited head.

These numbers are historical execution evidence, not a substitute for rerunning
the gates. A third-party reviewer should execute at least:

```bash
make ci
make lint
make docs-check
go test -race ./ -count=1
go test ./...
```

Optional cloud modules must also be tested with the same build tags and provider
environment used by the consuming binary. Generated Graphify output is ignored
by repository extraction and is a navigation aid, not proof of correctness.

## 11. Known limitations, deliberate exclusions, and open findings

### 11.1 Open process finding: missing DCO trailer

Commit `c179a77e7c0f2f3cb6e6a5dee06766dccb09196b` has a valid cryptographic
signature but no `Signed-off-by` trailer. A cryptographic signature and DCO
sign-off prove different things. If the pull request enforces the repository's
DCO workflow, this commit must be corrected through an explicitly authorized
history rewrite or another repository-approved remediation before merge.

### 11.2 V2 has not established a released API baseline

The compatibility workflow cannot compare this branch against a prior v2
release because none exists. This is expected for the first v2 release, but it
means API-diff automation has not independently proven stability within the v2
major. After v2.0.0, that release should become the baseline.

### 11.3 Provider certification boundary

The repository has live Token/AppRole/KV interoperability coverage with
OpenBao. AWS IAM, Azure managed identity, GCP, Kubernetes, LDAP, JWT, and OIDC
have protocol/unit fixtures and constructor/self-config coverage, but this
dossier does not claim every method was live-certified against every provider
deployment. Identity systems depend on deployment metadata, trust policy,
redirect URIs, and cloud credentials that unit tests cannot certify.

### 11.4 Persisted version compatibility

The v2 version manager intentionally does not load v1 timestamp/hash filenames
or records. Consumers must create a new v2 snapshot baseline after upgrade.

### 11.5 Rollback and secret refresh semantics

A version stores materialized values. Rollback restores those values without
provider I/O and has no unresolved references for `RefreshSecrets` to revisit.
Use a non-incremental source Reload to leave the historical rollback state and
materialize current provider values.

### 11.6 Optional cloud module and build-tag boundary

Cloud loaders and secret stores remain independent modules and are guarded by
provider build tags where appropriate. Importing a cloud module without the
matching production tags can result in “build constraints exclude all Go
files.” This is intentional dependency selection, not automatic inclusion.

### 11.7 Explicit deadline ownership

Context-free operations receive Confii's finite defaults. An explicit caller
context with its own deadline wins. If a caller deliberately supplies an
unbounded context while disabling the fallback, continuous revision conflicts
or a non-returning provider can remain unbounded; the caller owns that choice.

### 11.8 Format strictness is intentional

V2 does not use parser fallback to rescue mislabeled input. `.yml` and `.cfg`
remain valid filename extensions for canonical `yaml` and `ini`, respectively,
but declarative aliases and cross-format content are rejected. This can break
previously tolerated configuration and is an intended hardening change.

### 11.9 No claim based solely on generated analysis

Graphify was used to discover relationships and orphan/duplication candidates.
Every accepted code claim was reconciled with source and tests. `graphify-out/**`
is excluded from extraction, and generated graph counts or communities are not
correctness guarantees.

### 11.10 Documentation describes the final tree

Some intermediate implementations were replaced later in the same branch.
Current documentation describes the final tree, not each historical snapshot.
This dossier records supersession explicitly so reviewers do not audit deleted
transaction code as the supported design.

## 12. Reviewer-focused risk checklist

An independent reviewer should pay particular attention to:

1. lock ownership and revision checks immediately before every publication;
2. candidate completeness when new fields are added to `Config[T]`;
3. whether all callbacks and synchronous listeners remain outside `c.mu`;
4. defensive copying at public, observer, version, and validator boundaries;
5. secret error classification, especially fallback only on not-found;
6. cache invalidation on refresh, rollback, and hook-plan changes;
7. source-format mismatch and polyglot-input tests;
8. explicit provider credential precedence and rotation behavior;
9. initializer symlink, traversal, ambiguity, preflight, and rollback handling;
10. error wrapping through `ConfigError.Err` and absence of message parsing;
11. optional-module tests with real build tags;
12. the missing DCO trailer identified above.

## 13. How to challenge a claim in this dossier

For any row or section:

1. identify the final implementation file in section 9;
2. use `git blame` to find the responsible commit;
3. inspect that commit and all later commits in the audited range;
4. run the named focused tests with `-count=1` and, for concurrency paths,
   `-race`;
5. construct a negative test that attempts partial publication, stale overwrite,
   callback reentry, cross-format parsing, or secret fallback on a non-not-found
   error;
6. treat a mismatch between this document and executable behavior as a defect
   in this dossier or the implementation, not as undocumented flexibility.

The purpose of this record is to make such challenges inexpensive and to make
unsupported or hallucinated implementation claims visible before release.
