# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.1.x   | Yes                |
| < 1.1   | No                 |

## Reporting a Vulnerability

We take security seriously. If you discover a vulnerability in Confii, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please report vulnerabilities by emailing:

**confii.connect@gmail.com**

Include the following in your report:

- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Potential impact
- Suggested fix (if any)

## Response Timeline

- **Acknowledgment:** Within 48 hours of receiving your report
- **Initial assessment:** Within 5 business days
- **Fix timeline:** We aim to release a patch within 30 days for confirmed vulnerabilities
- **Disclosure:** We will coordinate public disclosure with you after a fix is available

## Scope

The following are in scope:

- The `confii-go` library code (`github.com/confiify/confii-go`)
- Secret store integrations (credential handling, caching, resolution)
- Configuration parsing (injection via crafted config files)
- The CLI tool (`confii`)

The following are out of scope:

- Vulnerabilities that exist only in third-party code and are not reachable
  through Confii. Reports are still welcome; we will update dependencies or
  coordinate with the upstream maintainer when appropriate.
- Issues that require physical access to the machine
- Denial of service via extremely large config files (expected behavior)

## Recognition

We appreciate security researchers who help keep Confii safe. With your permission, we will acknowledge your contribution in the release notes for the fix.

## Security Best Practices for Users

- Never commit secrets or credentials in configuration files
- Use `${secret:key}` placeholders with a proper secret store in production
- Enable `WithFreezeOnLoad(true)` in production to prevent runtime config mutation
- Use build tags to include only the cloud providers you need
- Keep your Go toolchain and Confii version up to date

---

## Threat Model & Mitigations

A configuration library sits between untrusted inputs (config files,
env vars, remote stores) and trusted application state. The threats
enumerated below are the ones Confii actively defends against. Each
mitigation is a structural property of the library — pinned by a
corresponding regression test — rather than a runtime check that a
caller must remember to enable.

For the design reasoning behind these mitigations, see
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

### Memory-safety threats

**T-1. Aliased state through value isolation boundaries.** A caller
that passes a value to `Set`, `Override`, or a loader must not be
able to mutate Config-internal state by retaining and modifying the
original value. The same applies in reverse: a caller that receives
a value from `Get`, `Export`, or the source tracker must not be able
to mutate Config-internal state by modifying the returned value.

> *Mitigation.* Every isolation boundary delegates to the
> reflection-based deep-copy engine in `internal/dictutil`, which
> handles every `reflect.Kind` (typed slices, typed maps, pointers,
> structs, byte slices, arrays) and detects cycles via a
> `(type, allocation address)` visited set. Channels, function
> values, and unsafe pointers are returned by reference because they
> carry no Go-level state a caller can mutate through the returned
> interface.

**T-2. Aliased state through introspection APIs.** A caller that
inspects the source tracker via `GetSourceInfo`,
`GetOverrideHistory`, `GetConflicts`, or `ExportDebugReport` must
not be able to poison tracker state by mutating fields of the
returned `*SourceInfo` (notably `Value`, which is `any`).

> *Mitigation.* `cloneSourceInfo` routes both `SourceInfo.Value` and
> every `OverrideEntry.Value` through the deep-copy engine before
> escaping a `*SourceInfo` to a caller.

### Concurrency threats

**T-3. Data races on observability paths.** A debug-report or
statistics serializer that releases the lock before marshaling
against live pointers races concurrent writers and trips `-race`.

> *Mitigation.* All `Export*` / `Statistics*` routines build a
> deep-copied snapshot under the read lock, release the lock, and
> marshal the private snapshot. The lock is held only for the
> snapshot, not the encode.

**T-4. Initialization races on lazily-configured singletons.** Two
goroutines racing on a lazy `sync.Once` initializer can drop a
caller's configuration arguments — the second `Once.Do` is a no-op
even if its arguments differ.

> *Mitigation.* Lazy initialization that accepts caller-supplied
> arguments uses a plain `sync.Mutex`-guarded path with an
> idempotent reconfigure step rather than `sync.Once`. The most
> recent caller's arguments always take effect.

### State-integrity threats

**T-5. Atomic-rollback drift.** A phased operation (`Reload`,
`Extend`, `Override`) that mutates multiple internal components and
then fails partway must restore *every* component to its pre-call
state, not just some. Partial rollback leaves the Config in a state
no caller asked for, which is observable as "phantom" behavior on
the next operation.

> *Mitigation.* Every component touched by a phased operation
> exposes `Snapshot` / `Restore` and participates in a single
> rollback closure: `envConfig`, `mergedConfig`, `sourceTracker`,
> `fileTracker`, and the override-stack base.

**T-6. Phantom resurrection in nested overrides.** A caller that
holds restore closures for two nested `Override` calls and pops the
inner one expects the outer to remain applied; popping the outer
expects the inner — if still active — to remain applied. A naive
"capture envConfig at push, write it back at pop" implementation
violates this and resurrects already-popped values.

> *Mitigation.* `Config.Override` maintains an internal LIFO stack
> of frames. The captured base is taken once on the empty →
> non-empty transition; restore removes the named frame from any
> stack position and rebuilds live state by replaying remaining
> frames onto the base. Restore is idempotent.

### Input-handling threats

**T-7. Silent map-key collisions in YAML.** A YAML document with
distinct typed keys that share a stringified projection (`true` vs
`"true"`, `1` vs `"1"`, `1.0` vs `1`) coerced through `fmt.Sprint`
silently drops one entry — random map iteration order picks the
winner. The data loss is undetectable from the loaded config.

> *Mitigation.* `dictutil.NormalizeKeys` projects map keys through a
> typed per-kind encoder (`strconv.FormatInt` /
> `strconv.FormatUint` / `strconv.FormatFloat` / canonical
> "true"/"false"). Two distinct source keys producing the same
> coerced string surface a typed `*KeyCollisionError` propagated as
> a YAML format error.

**T-8. Type drift on change notification.** A change-callback that
compares old and new values via `fmt.Sprintf("%v", …)` suppresses
type drift: `int(8080)` and `float64(8080)` both stringify to
`"8080"` and the diff is hidden, even though downstream consumers
type-assert and panic on the unexpected concrete type.

> *Mitigation.* All change-detection equality flows through
> `reflect.DeepEqual`. `fmt.Sprint` is for humans, never for
> equality or deduplication.

**T-9. Path traversal in file-backed secret stores.** A
file-backed secret provider that interprets user-controlled
`${secret:KEY}` placeholders as file paths must reject keys
containing `..`, leading `/`, or null bytes — otherwise an
adversarial config can read arbitrary files outside the configured
base directory.

> *Mitigation.* The shipped `file` self-config secret provider
> rejects path-traversal keys before any filesystem access. New
> path-interpreting providers must follow the same pattern; see the
> Self-Config Secret Registry section in
> [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

### Operational-visibility threats

**T-10. Silent callback panics.** A `recover()` block that does not
log the recovered value hides a misbehaving callback indefinitely.
Operators have no signal a handler is broken until downstream
consumers go dark.

> *Mitigation.* Change-callback dispatch goes through
> `invokeChangeCallback`, which logs the panic value, the affected
> key, the callback registration index, and `runtime/debug.Stack()`
> at error level on `c.logger`. Sibling callbacks continue
> regardless. `_ = recover()` is treated as a code smell across the
> codebase.

### Verification

- `go test ./...` — full suite passes.
- `go test ./... -race` — full suite passes across all 21 packages
  with no detected races.
- 48 negative tests pin the threat-mitigation contracts above and
  would fail against pre-mitigation code. Test files are named
  `*_v23_test.go` / `*_v24_test.go` adjacent to the source they
  pin.

### Reporting a regression

Treat a regression of any threat above as a security report and
follow the disclosure process at the top of this document. Each
mitigation is pinned by a regression test; if a regression slipped
past CI, the pin is missing or weakened, which is itself a bug
worth reporting.
