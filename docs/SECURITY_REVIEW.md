# Security Review and Assurance Case

**Review date:** 2026-07-25

**Scope:** `github.com/confiify/confii-go`, its CLI, separately versioned cloud
loader and secret modules, build/release workflows, and public documentation

**Acceptance:** maintainer review and merge of the pull request that introduced
this document

This is the project's security review and assurance case. It records the
security boundary, requirements, evidence, findings, and residual risks. It is
reviewed after a material boundary change and at least once every five years.

## Security boundary

Confii accepts configuration files, environment variables, HTTP and cloud
responses, secret placeholders, schemas, CLI arguments, and programmatic values
from a host application. Those inputs are untrusted until parsed and validated.
Confii produces in-process configuration state, typed values, diagnostics,
exports, callbacks, and CLI output. The host process, operating system,
transport, cloud identity, and upstream secret stores are outside the trust
boundary; Confii must use their supported security interfaces without exposing
credentials or resolved secrets.

The core module intentionally excludes cloud SDKs. Cloud loaders and secret
stores are separate modules and build-tag-selected integrations, reducing the
dependency and credential surface for applications that do not need them.

## Security requirements

1. Parsing and normalization must reject ambiguous or malformed input without
   silently losing data.
2. State transitions and reloads must be atomic, concurrency-safe, and isolated
   from caller-owned mutable values.
3. Secret values and credentials must not be logged, exported accidentally, or
   committed to the repository.
4. Path-like and environment selectors must reject traversal and unsafe names.
5. Remote transports must use the Go standard library or maintained provider
   SDKs with certificate verification enabled; Confii does not implement its
   own cryptographic algorithms.
6. Published code and artifacts must be traceable to reviewed, verified source.
7. Known vulnerabilities and security regressions must be handled through the
   private response process and permanently covered where practical.

## Review method and evidence

The review combined manual boundary and data-flow analysis with:

- the threat model and regression-test mapping in `SECURITY.md`;
- CodeQL, `go vet`, `golangci-lint`, Govulncheck, OSV Scanner, and dependency
  review;
- Gitleaks scanning of the release tree;
- unit, integration, negative, race-detector, and cloud-consumer tests;
- native Go fuzz targets for file parsing, environment values, merging,
  normalization, type coercion, and secret resolution;
- protected-branch, code-owner, verified-signature, and required-check controls;
  and
- signed immutable tags, pinned release automation, checksums, and artifact
  provenance.

## Findings and disposition

- Ambiguous YAML key coercion, mutable aliasing, partial rollback, callback
  panic visibility, and provider-version propagation were hardened and pinned
  by regression tests during the v1.1.0 security pass.
- A clear-text secret logging example identified by CodeQL was removed; examples
  demonstrate use without printing resolved secret values.
- OpenTelemetry GO-2026-5158 / CVE-2026-41178 was detected in opt-in cloud
  dependency graphs and upgraded to the fixed v1.44.0 release.
- Release provenance and signed-tag enforcement were added so consumers can
  connect artifacts to reviewed source.
- No unresolved critical or high-severity finding is accepted for release.

## Cryptography and transport

Confii contains no custom cryptographic primitive, password database, or random
number generator. TLS, certificate validation, cloud authentication, and Vault
transport are delegated to the supported Go standard library and maintained
provider SDKs. Go's supported TLS stack negotiates current algorithms and
rejects invalid certificates by default. Options that deliberately weaken
verification are not exposed by Confii's public configuration API.

## Residual risks

- Applications can misuse returned configuration values or intentionally
  install unsafe custom loaders and secret stores.
- Availability depends on remote providers and caller-selected timeouts.
- Build tags and optional modules reduce, but cannot eliminate, upstream
  dependency risk.
- Fuzzing and static analysis improve confidence but do not prove absence of
  defects.

These risks are documented rather than hidden. Users should apply least
privilege to provider credentials, pin supported releases, avoid storing raw
secrets in configuration, and follow `SECURITY.md` when a regression is found.
