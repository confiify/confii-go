# Architecture Guide

This document describes the structural invariants the confii-go
codebase relies on. It is intended for contributors and reviewers; if
you are looking to *use* the library, start with the
[README](https://github.com/confiify/confii-go#readme) and the
[documentation home](index.md).

The invariants below are not "rules of style." They are load-bearing
properties that other parts of the codebase depend on for correctness.
Violating one is more likely to surface as a hard-to-reproduce
production bug than as a CI failure.

---

## Table of Contents

0. [Root Package Layout](#0-root-package-layout)
1. [The DeepCopy Engine](#1-the-deepcopy-engine)
2. [The Override Stack](#2-the-override-stack)
3. [Self-Config Provider Registries](#3-self-config-provider-registries)
4. [Cross-Cutting Invariants](#4-cross-cutting-invariants)

---

## 0. Root Package Layout

The module root is the public `github.com/confiify/confii-go/v2` package. Go
packages are directory-based, so the public facade and its package tests stay
at the repository root. Files are divided by responsibility; moving a public
type into a subdirectory would create a different import path and is therefore
an API change.

| File | Responsibility |
|---|---|
| `config.go` | `Config[T]`, construction, and core state invariants |
| `config_load.go` | loader execution and layer-cache rebuilding |
| `config_access.go` | getters, typed access, defensive read snapshots |
| `config_mutation.go` | `Set`, runtime mutation, and change callbacks |
| `config_inspect.go` | source inspection, documentation, and export |
| `config_reload.go` | full/incremental reload candidate preparation and validation gates |
| `config_extend.go` | runtime loader-extension candidate preparation |
| `config_source_transaction.go` | shared Reload/Extend capture, publication, retry, and lifecycle signals |
| `config_override.go` | composable override stack and restoration |
| `config_lifecycle.go` | freezing, environment identity, and file watching |
| `config_observe.go` | metrics, events, drift, and version management |
| `config_hooks.go` | hook traversal and hook-facing accessors |
| `config_self.go` | self-configuration application |
| `config_validation.go` | JSON Schema and typed validation orchestration |
| `config_file_loader.go` | self-config-discovered local file parsing |
| `config_copy.go` | snapshot copies shared by transactional operations |

New behavior belongs in the narrowest existing responsibility file. Do not
create generic `util`, `misc`, or `helpers` files; a helper should live with
the behavior that owns it unless several transaction paths genuinely share it.

### Public key-path operations

**Package:** `configmap`

The dependency-free `configmap` package owns Confii's dot-separated map path
contract. Its `Get`, `Set`, `Has`, and `Keys` functions are available to
custom loaders, validators, exporters, and independently versioned Confii
modules. `Set` is atomic on error and returns typed categories for invalid
paths, nil maps, and scalar/map conflicts. `Keys` is deterministic and returns
fully qualified paths that can be passed back to the other operations.

Core code may retain narrow internal wrappers where that keeps unrelated
dictionary behavior cohesive, but it must not reimplement path parsing or
mutation. Optional cloud modules import `configmap` rather than maintaining a
provider-local path implementation.

Root tests follow the same rule. Public contract tests use `package
confii_test` by default. Tests use `package confii` only when they must inspect
private state or exercise an internal failure path. Test names and comments
describe the behavior and contract being verified without relying on internal
audit identifiers. Cross-package end-to-end tests belong under `integration/`;
there is no generic top-level `tests/` package.

---

## 1. The DeepCopy Engine

**Package:** `internal/dictutil`
**Internal API:** [`DeepCopy`](https://github.com/confiify/confii-go/blob/main/internal/dictutil/copy.go),
[`DeepCopyValue`](https://github.com/confiify/confii-go/blob/main/internal/dictutil/copy.go)

### Why a reflection engine

The library's `Config[T]` accepts arbitrary user values through
`Set`, `Override`, and the loader pipeline, and returns values
through `Get`, `Typed`, `Export`, and the source tracker. Both
boundaries must isolate caller-owned state from internal state — a
caller mutating a slice it passed to `Set` must not corrupt
`envConfig`, and a caller mutating a slice it received from `Get`
must not corrupt the read-side cache.

A typed switch over the shapes a JSON / YAML decoder produces
(`map[string]any`, `[]any`) is insufficient. Real callers pass
`[]byte`, `[]string`, `map[string]int`, custom structs, pointers
into shared graphs, and occasionally cyclic structures. A handler
that misses any of these silently aliases caller state into Config
state.

The reflection engine handles every Go value uniformly. There is
exactly one place in the codebase that decides "how do I clone an
arbitrary value?" and every isolation boundary delegates to it.

### The `reflect.Kind` strategy

`deepCopyRV` switches on `reflect.Kind`:

| Kind | Strategy |
|---|---|
| `Bool`, all int/uint/float/complex, `String` | passthrough — primitives are immutable |
| `Chan`, `Func`, `UnsafePointer` | passthrough — no reachable Go-level state to clone |
| `Pointer` | allocate via `reflect.New(elem.Type())`; recurse into `Elem()`; visited-set detects cycles |
| `Slice`, `Array` | element-by-element; primitive-element fast path uses a single `reflect.Copy` |
| `Map` | `MakeMapWithSize`; recurse keys and values; visited-set detects cycles |
| `Interface` | recurse into the concrete inner value, repackage into the original interface type |
| `Struct` | bit-copy first; overwrite settable exported fields with deep clones; `time.Time` is a special case |

### The struct bit-copy-then-overwrite tradeoff

Go's `reflect` package cannot `Set` an unexported field —
`Value.CanSet()` returns false. A naive "deep-copy every field"
strategy would either:

1. Skip unexported fields, leaving them zero-valued in the clone.
   This silently corrupts any struct whose state lives behind
   unexported fields (`sync.Mutex`, custom buffered types).
2. Use `unsafe` to write through the field offset. Fragile across Go
   versions and likely to trip the race detector.

The bit-copy-then-overwrite approach gets the best of both:
unexported fields are preserved verbatim (so a `sync.Mutex` carrying
its lock state survives the clone), and every settable exported
field is then overwritten with a deep clone. Unexported reference
fields remain shallow — the engine cannot do better without unsafe
access — but the public surface a caller can mutate through the
struct's API is fully isolated.

### The `time.Time` carve-out

`time.Time` has three unexported fields: `wall uint64`, `ext int64`,
`loc *Location`. The first two are immutable values; the third is a
pointer to globally-shared, immutable state in the `time` package. A
plain bit-copy is both safe and necessary — `reflect.Value.Set`
cannot reach the unexported `loc` field, so descending into struct
field iteration would fail. The engine recognizes `time.Time`
explicitly and returns a bit-copy clone.

This is the only type-specific carve-out in the engine. Every other
struct type goes through the generic bit-copy + overwrite path.

### Cycle detection

A user-supplied value graph can be self-referential:

```go
a := &Node{Name: "a"}
b := &Node{Name: "b"}
a.Next = b
b.Next = a // cycle
```

The engine carries a `visited map[visitedKey]reflect.Value` keyed by
`(reflect.Type, allocation address)` for every reference-typed
visit. On a second visit, the engine returns the previously-allocated
clone instead of recursing again. This both terminates and
**preserves shared-substructure identity** — two pointers in the
source graph that aliased the same allocation still alias the same
allocation in the clone.

The compound `(type, address)` key is conservative: two distinct
allocations can occupy the same address at different times (after a
GC), and two values of different types cannot be the same allocation
in a valid Go program.

### Boundaries that delegate to the engine

Every isolation boundary in the codebase routes through one of the
two public entry points:

| Boundary | Direction | Entry point |
|---|---|---|
| `Config.Set` / `Config.Override` | caller → Config | `DeepCopyValue` on the value before storage |
| `Config.Get` / `Config.GetWithContext` | Config → caller | `DeepCopyValue` on the published snapshot value |
| `Config.Export*` | Config → external | `DeepCopy` on a snapshot taken under lock |
| `sourcetrack.Tracker.Snapshot` | live → snapshot | `cloneSourceInfo` calls `DeepCopyValue` on each `Value` |
| `sourcetrack.Tracker.Get*` (`SourceInfo`, `OverrideHistory`, `Conflicts`) | live → caller | same |
| Override-stack base capture | live → frame base | `DeepCopy` on `envConfig` and `mergedConfig` |
| `Config.RollbackToVersion` | snapshot → live | `DeepCopy` on `Version.Config` |

The rule for new code: **if you are returning a value that came out
of internal state, or accepting a value that will become internal
state, route it through `dictutil.DeepCopyValue`.** Do not skip the
copy on the assumption that the caller "won't mutate it." Aliasing
is a class of bug, not a single bug.

---

## 2. The Override Stack

**Source:** [`config_override.go`](https://github.com/confiify/confii-go/blob/main/config_override.go), `Config.Override` and
`makeOverrideRestore`.

`Config.Override` applies a layer of key/value mutations and returns
a `restore func()` that undoes them. Multiple overrides may be
active concurrently. The stack discipline below guarantees that
restoring any frame leaves the Config in the state it would have
been in had that frame never been pushed, regardless of pop order.

### The composability requirement

A naive implementation captures `envConfig` at push time and writes
that snapshot back at pop time. Under nested overrides this produces
a phantom-resurrection failure mode:

```go
cfg.Set("k", "base")
rA, _ := cfg.Override(map[string]any{"k": "A"})
rB, _ := cfg.Override(map[string]any{"k": "B"})
rA()                     // naive: writes snapshot-of-base; "k" = "base"
rB()                     // naive: writes snapshot-after-A; "k" = "A" — phantom
```

`rB`'s captured snapshot already had `A` applied, so writing it
back resurrects `A`. The contract a caller expects — "popping a
frame undoes only that frame" — is violated.

### The stack-based design

`Config[T]` carries:

```go
overrideStack       []*overrideFrame
overrideBaseEnv     map[string]any
overrideBaseMerged  map[string]any
overrideBaseTracker sourcetrack.Snapshot
overrideBaseFrozen  bool
```

The base is captured exactly once, when the stack transitions from
empty to non-empty. It is consumed exactly once, when the stack
drains back to empty. While the stack is non-empty, live envConfig /
mergedConfig / sourceTracker are derived by replaying remaining
frames onto the captured base.

Each frame carries:

```go
type overrideFrame struct {
    id      uint64
    payload map[string]any
    applied bool
}
```

#### Push

1. Acquire `c.mu`.
2. If the stack is empty, capture the base — deep copies of
   `envConfig` and `mergedConfig`, plus a tracker snapshot and the
   current `frozen` value.
3. Build a frame with a deep-copied payload and apply it via
   `dictutil.SetNested` against live `envConfig` and `mergedConfig`.
   Track each key with source label `"override"`.
4. Push the frame. Set `c.frozen = false` so a frozen Config
   tolerates nested overrides while in scope.
5. Release `c.mu`. Fire OnChange callbacks.

#### Pop (`makeOverrideRestore(frame)`)

The restore closure removes the frame **regardless of stack
position**:

1. Acquire `c.mu`.
2. If `frame.applied == false`, return — restore is idempotent.
3. Flip `frame.applied = false` and remove the frame from
   `overrideStack`.
4. **Stack drained:** restore `envConfig` / `mergedConfig` from the
   captured base directly, restore the tracker, restore `frozen`,
   clear the base fields.
5. **Surviving frames:** rebuild `envConfig` and `mergedConfig` from
   a fresh deep copy of the captured base (so the next replay does
   not mutate the persisted base), restore the tracker, then for
   each remaining frame in original push order replay its payload
   via `SetNested` + `TrackValue`. Replay-time `SetNested` failures
   (which would mean `Set` or `Reload` during override scope
   reshaped the tree) are logged and skipped — best-effort under
   concurrent mutation.

After rebuilding the override stack, invalidate `validatedModel`, release the
lock, and fire OnChange callbacks against the diff between pre-restore and
rebuilt state.

### Why "rebuild from base" instead of "apply inverse delta"

Two designs are possible:

- **Inverse-delta:** each frame stores the values that existed
  before its override applied. Restore applies the inverse delta to
  the live state.
- **Replay-from-base:** the captured base plus the remaining frames
  are the source of truth. Restore rebuilds from the base.

Replay-from-base wins on three counts:

1. **Single source of truth.** The base is captured once and never
   mutated. The live state is *always* equal to base + active
   frames replayed in order. There is no inverse-delta bookkeeping
   that can drift.
2. **Resilience to interleaved mutation.** If the caller invokes
   `Set` or `Reload` during override scope, the inverse-delta
   approach has to choose whether the inverse "wins" against the
   intervening change. Replay-from-base has a clean semantic: the
   base is the source of truth; intervening mutations are wiped on
   restore.
3. **Idempotent rebuild.** Replaying frames in push order is
   deterministic regardless of pop order.

The cost is a one-time `OverrideCount` increment per replay through
`TrackValue`. This is documented in the `Override` godoc and is a
deliberate tradeoff against the implementation surface of inverse-
delta bookkeeping.

### Idempotency

The `applied bool` flag is mutated only under `c.mu`, so concurrent
restore invocations on peer frames cannot race. A caller that
defers `restore()` and also calls it manually gets exactly one
rebuild plus a no-op.

---

## 3. Self-Config Provider Registries

### Configuration source registry

**Source:** [`config_source_self.go`](https://github.com/confiify/confii-go/blob/main/config_source_self.go).

Optional cloud loaders use the same driver-registration boundary as secret
stores. Root owns the context-aware factory contract but does not import any
provider SDK:

```go
type SelfConfigSourceProviderFactory func(
    context.Context,
    map[string]any,
) (Loader, error)

func RegisterSelfConfigSourceProvider(name string, factory SelfConfigSourceProviderFactory)
func LookupSelfConfigSourceProvider(name string) (SelfConfigSourceProviderFactory, bool)
```

Core file/environment sources stay in the built-in dispatcher. The opt-in
`loader/cloud` module registers `git`, `s3`, `ssm`, `azure_blob`, `gcs`, and
`ibm_cos`; provider SDK implementations are selected by build tags. `New`
passes its caller context into a registered factory and later into
`Loader.Load`, so deployment preflights and application startup use the same
credential discovery and read path.

Unknown providers fail closed and list the factories actually registered in
the current binary. A factory error is wrapped as `ErrConfigLoad`, and a nil
loader is rejected before loading begins.

### Secret registry

**Source:** [`config_secret_self.go`](https://github.com/confiify/confii-go/blob/main/config_secret_self.go).

The declarative `secrets:` block configures named provider aliases, selects a
default globally or by active environment, and routes
`${secret@provider:key}` explicitly. The available provider types vary with
build configuration and registered provider factories.

### The constraint

`secret/cloud/*` (AWS, Azure, GCP, Vault, IBM) all import the root
`confii` package — they need types like `confii.SecretOption` and
`confii.ErrSecretNotFound`. Therefore root cannot directly import
`secret/cloud/*` without a cycle.

A naive switch in root over hard-coded provider names supports only
providers root knows about, which excludes every cloud store. This
leaves a usability cliff: users with imperative `secret.Resolver`
wiring cannot migrate to declarative self-config without losing
their cloud secret backend.

### The driver registry pattern

The library uses the canonical `database/sql` driver pattern: a
sync.Map indexed by lowercase provider name, populated by `init`
functions in the relevant packages.

```go
// Public API in root:
type SelfConfigSecretProviderFactory func(
    context.Context,
    map[string]any,
) (SecretReader, error)

func RegisterSelfConfigSecretProvider(name string, factory SelfConfigSecretProviderFactory)
func LookupSelfConfigSecretProvider(name string) (SelfConfigSecretProviderFactory, bool)

type SecretRequest struct {
    Key     string
    Field   string
    Version string
}

type SecretReader interface {
    ReadSecret(context.Context, SecretRequest) (any, error)
}
```

Named provider factories are validated at construction and initialized only
when the selected effective environment references them. After consolidation,
Confii eagerly materializes all remaining secret references before publishing
the Config. Reads of the same provider/key/version document are deduplicated
within that materialization session, so multiple JSON paths share one backend
fetch. The unresolved selected snapshot is retained for provider attribution
and explicit `RefreshSecrets`; ordinary access uses the resolved snapshot.
JSON-path extraction is performed uniformly after the store read;
every adapter receives the same request-aware contract, including an optional
version and field. There is no runtime capability detection or legacy
key-only provider interface in v2.

Three providers are pre-registered by root's own `init()`:

- **`env`** — OS environment variable backed.
- **`dict`** — in-memory map seeded from a `secrets.entries:` block.
  Useful for tests, examples, and local development.
- **`file`** — Docker / Kubernetes secret-mount style. Each
  `${secret:KEY}` placeholder maps to `base_dir/KEY[extension]`.
  Includes a path-traversal guard rejecting `..`, absolute paths,
  and null bytes.

External providers register themselves at import time:

```go
//go:build aws

package cloud

import (
    "context"

    confii "github.com/confiify/confii-go/v2"
)

func init() {
    confii.RegisterSelfConfigSecretProvider("aws", func(ctx context.Context, cfg map[string]any) (confii.SecretReader, error) {
        store, err := NewAWSSecretsManager(ctx, optionsFrom(cfg)...)
        if err != nil { return nil, err }
        return readOnlySelfConfigAdapter(store), nil
    })
}
```

The user opts in with a blank import:

```go
import (
    _ "github.com/confiify/confii-go/secret/cloud/v2" // selected by build tag
)
```

When the matching build tag is set (e.g. `-tags=aws`), the cloud
package's `init()` runs and registers the factory. When the tag is
absent, the cloud package compiles out and the provider name is not
in the registry.

### Build-tag configuration errors

`buildSelfConfigSecretHook` lists the currently-registered names in
the unsupported-provider error:

```text
self-config secrets provider "aws" unsupported (registered: dict, env, file);
cloud providers must be opted in via a build-tagged blank import of secret/cloud
```

An operator running `go build` without `-tags=aws` and then declaring an AWS
provider under `secrets.providers` sees both the available providers
and the path to opt in.

### Writing a new provider

Three contracts must hold:

1. **Factory signature.**
   `func(context.Context, map[string]any) (SecretReader, error)`. The map is
   one provider entry under `secrets.providers`. Validate required fields
   (e.g. `base_dir`) and return a typed error from the factory if
   they are missing — `buildSelfConfigSecretHook` wraps that error
   into a `*ConfigError`.
2. **`ReadSecret` is read-only and goroutine-safe.** It runs from
   the hook pipeline under no particular caller lock. Cache or
   fetch on demand behind `sync.RWMutex` or `sync.Map`.
3. **Path-traversal-safe key handling.** If your store interprets
   keys as paths (file, S3 prefix, KV mount), reject keys
   containing `..`, leading `/`, or null bytes before lookup. The
   shipped `file` provider has a reference implementation.

Register from your package's `init()`. Document the `secrets:`
block schema your factory expects in your provider's godoc.

---

## 4. Cross-Cutting Invariants

These rules apply everywhere; violations are the most common source
of regression.

### Atomic state consistency

`Reload` and `Extend` mutate private candidates and publish them only after all
loading, materialization, and validation succeeds. `Override` mutates live
state under the write lock and therefore retains an explicit rollback path.
Transactional state currently includes:

- `unresolvedEnvConfig` / `envConfig` / `mergedConfig`
- ordered `loaders`, composed `loaderLayers`, and `loaderDependencies`
- `sourceTracker` (via `Snapshot` / `Restore`)
- `fileTracker` (via `Snapshot` / `Restore`)
- `sourcePlan` and the typed-model cache generation
- override-stack base (when applicable)

If you add a new component to `Config[T]`, audit every phased operation. State
derived from source loading must be copied by `snapshotSourceCandidate` and
published by `publishSourceCandidate`. Live mutation paths must either operate
on a private candidate or restore the component explicitly on failure.

### Lock-then-snapshot for observability

Any `Export*`, `GetSnapshot*`, `Statistics*`, or similar routine
that serializes internal state must:

1. Acquire the read lock.
2. Build a deep-copied snapshot of the state it intends to render.
3. Release the read lock.
4. Marshal / serialize / format the snapshot.

Releasing the lock before marshaling against live pointers is a
data race; marshaling under the lock blocks every writer for the
encode duration. The deep-copy-then-release pattern is the
balance.

### Type-aware comparison and coercion

`fmt.Sprint` / `fmt.Sprintf("%v", …)` is for humans, never for
equality or deduplication.

- For value equality, use `reflect.DeepEqual`. It is type-aware:
  `int(8080)` and `float64(8080)` are not equal, so a JSON-decoded
  float replacing an int correctly produces a diff.
- For coercing a non-string value to a map-key string, use a typed
  per-kind encoder (`strconv.FormatInt`, `strconv.FormatFloat`,
  etc.) and surface ambiguity as an error rather than picking a
  winner. See [`internal/dictutil/normalize.go`](https://github.com/confiify/confii-go/blob/main/internal/dictutil/normalize.go)
  for the reference implementation.

### Panic transparency

Any `recover()` block that does not log the recovered value is
hiding a bug. The change-callback dispatch logs the panic value,
the affected key, the callback index, and `runtime/debug.Stack()`
at error level on `c.logger`. Sibling-isolation property is
preserved.

If you add a new `recover()` somewhere:

1. Log the panic at error level with enough context for an operator
   to identify which subsystem failed.
2. Include `runtime/debug.Stack()` so the operator can see the
   actual failure site.
3. Document why a panic is being recovered rather than propagated —
   this is an isolation decision (e.g. "siblings continue"), not a
   reflex.

`_ = recover()` is a code smell. It is acceptable only in narrow,
documented cases (probing whether a value is comparable in
`hook/processor.go:isComparable`).
