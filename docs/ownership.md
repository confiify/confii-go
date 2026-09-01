# Configuration Ownership

Confii is designed to be the only configuration engine an application needs. This
page states what that means as a contract: what Confii owns, what remains yours,
and where the boundary is deliberately drawn.

If you find yourself building a layer around Confii that re-implements anything
in the first table, that is a gap in Confii and worth reporting.

## What Confii owns

| Concern | Provided by |
|---|---|
| Source loading | `WithLoaders`, the `loader` package, declarative `sources` |
| Parsing | YAML, JSON, TOML, INI, `.env`, environment |
| Precedence | loader order, environment selection, `WithEnv` |
| Defaulting | `defaults` composition, struct tags |
| Merging | `WithMergeStrategy`, per-path strategies |
| Conflict detection | `GetConflicts`, `GetSourceStatistics` |
| Type admission | `Typed`, `TypedCopy`, `WithTypeCasting` |
| Unknown-field rejection | `WithRejectUnknownKeys`, `validate.Options` |
| Validation | `WithSchema`, `WithValidateOnLoad`, `Validator` |
| Secret-reference parsing | `secret.ParseReference`, `secret.Reference` |
| Secret resolution | `secret.Resolver`, declarative `secrets.providers` |
| Sensitivity | secret-reference detection, `WithSensitivePaths` |
| Redaction | `RedactedDict`, `ExportRedacted` |
| Provenance | `GetSourceInfo`, `Explain`, `GetOverrideHistory` |
| Immutable snapshots | `TypedCopy`, `Freeze` |
| Refresh | `RefreshSecrets`, `Reload`, watchers |
| Caching | resolver cache, `WithCacheTTL` |
| Lifecycle | `Close`, cascading to loaders, resolvers, and registered providers |

## What remains yours

| Concern | Why it cannot be ours |
|---|---|
| Your typed configuration structs | They are your domain, not ours |
| Domain cross-field validation | "end date after start date" needs your semantics |
| Which providers and policies are permitted | A deployment decision |
| Constructing runtime objects from a snapshot | Your database handle, not our concern |
| Mapping configuration errors to your exit codes | Your operational contract |
| Start-up and shutdown ordering | Only you know what depends on what |

## The snapshot ownership contract

`TypedCopy` returns a value that is yours. Specifically it:

- does not alias source maps or slices held by a loader
- does not alias resolver-returned maps, slices, or buffers
- does not alias another typed snapshot
- cannot be mutated in a way that writes back to configuration state
- is safe to read concurrently once returned
- does not change when a newer configuration is materialized
- does not change when secrets are refreshed into a newer snapshot
- remains readable and unchanged after `Close`

This holds for pointers, slices, arrays, maps, nested structs, interface-held
collections, and named collection types such as `type Hosts []string`. Named and
interface-held types are where an aliasing bug usually hides, because a copy
routine that switches on the concrete kinds it expects passes them through by
reference. `config_snapshot_ownership_test.go` mutates every one of these shapes
and asserts the configuration is untouched.

`Typed` is different and deliberately so: it returns a **cached** pointer, so two
calls may hand back the same value and a mutation through one is visible through
the other. It is the cheap read for code that will not mutate. Reach for
`TypedCopy` whenever you intend to hold or modify the result.

## Where the boundary sits for secrets

Confii resolves secrets, classifies them as sensitive, and offers surfaces that
redact them. It does not redact everywhere, and the difference is worth stating
plainly rather than leaving to be discovered.

Surfaces fall into three groups, and the group a surface is in is the thing to
know before you send its output anywhere.

**Resolved-value surfaces.** `Get`, `Typed`, `TypedCopy`, and `Export` are how
you read your configuration, and a resolved secret is what you asked them for.
They redact nothing, by design.

**Redaction-aware surfaces.** `RedactedDict`, `ExportRedacted`, `Explain`,
`Schema`, `GenerateDocs`, `GetSourceInfo`, `GetOverrideHistory`, `GetConflicts`,
the detached `SourceTracker`, and diffs replace a secret-backed or
declared-sensitive value with a marker. Reach for these when the destination is
a log, a support bundle, or anything you did not write yourself.
Secret-resolution errors are held to the same rule: they name the locator that
failed, never a value that resolved.
`TestSensitivePaths_HonoredByEveryDocumentedSurface` puts a plain literal marked
sensitive through each of them, which is how `GenerateDocs` was found to be
outside the group while looking like it was in it.

**Sensitive operational surfaces.** `PrintDebugInfo` and `ExportDebugReport`
report what a layer contributed, verbatim; their own API documentation says to
treat their output as sensitive. A saved version record stores the materialized
value alongside its sensitivity metadata. And provenance is only incidentally
safe on the ordinary load path — it shows `${secret:db/password}` because that
is what the layer held before resolution, not because anything redacted it.
After `RollbackToVersion` re-tracks a materialized snapshot it carries resolved
values; `TestProvenance_CarriesResolvedValuesAfterRollback` pins that.

Two things stay outside the boundary entirely:

**Bootstrap inputs.** A provider address, its CA material, and its
authentication material must exist before the first request and cannot
themselves be resolved from the provider. See
[Strict Vault configuration](secrets.md#strict-vault-configuration).

**Resolved values you have taken.** Once a secret is in your struct it is yours.
Confii cannot guarantee erasure of a value you have copied elsewhere, and no Go
library can.

### What `Close` actually disposes of

Narrower than "no cached copy remains", which is what an earlier version of this
page claimed:

- A `CloseableSecretResolver` is closed. It clears its cache, rejects later
  resolution with a typed error, and does not let work already in flight
  repopulate what it cleared.
- A resolver implementing only `ManagedSecretResolver` is **not** disposed of.
  It has no `Close` to call, and `Config.Close` does not fall back to
  `ClearCache`: that is an operational call a resolver may run arbitrary work
  in, not a disposal hook, and invoking it during shutdown re-enters `Close` for
  any resolver that transitions the configuration there. Its cache survives.
  Implement `CloseableSecretResolver` when a resolver holds secret material that
  must not outlive shutdown — that is what the interface is for.
- The materialized configuration is **not** cleared. A resolved secret already in
  the configuration stays there and stays readable — the snapshot contract above
  promises exactly that, and the two statements have to agree. `Close` bounds
  what Confii will go on to *do*, not what it still *holds*.

## Reporting a gap

If your application needs configuration behaviour that is not in the first table
and not in the second, that is a gap worth reporting rather than working around.
The point of this page is that the two tables should be exhaustive.
