# Changelog

The canonical, release-by-release changelog is maintained in
[CHANGELOG.md](https://github.com/confiify/confii-go/blob/main/CHANGELOG.md).

## Upcoming release

No changes are currently assigned beyond v2.0.0. Consult the Unreleased section
of the canonical changelog as post-release work begins.

## v2.0.0

Version 2.0.0 adopts semantic import versioning, the `confii` mapping tag, one
context-aware hook contract, eager validated snapshots, transactional runtime
changes, deterministic provider registration, canonical declarative sources,
qualified mixed-provider secrets, and structured errors. See the
[v2 migration guide](v2-migration.md) and the canonical changelog for the
complete release notes.

## v1.4.1

Version 1.4.1 hardens development-consumer cleanup after local module
replacement testing. It can pin all selected Confii modules to an explicit
release, removes synthetic zero pseudo-version requirements safely, and keeps
branch-coverage enforcement compatible with test-only Go package directories.
Consult the canonical changelog for complete release notes.

## v1.4.0

Version 1.4.0 adds eager transactional secret materialization, explicit mixed
secret-provider routing, environment-aware declarative cloud sources, and a
value-safe `confii connections test` preflight. It also adds `confii env` and
`Config.AvailableEnvironments()`, LocalStack-compatible S3/SSM endpoints,
commit-aware development installation and consumer linking, and a Vault OIDC
interoperability fix. Existing single-provider references and explicit loader
workflows remain compatible. Consult the canonical changelog for complete
release notes.

## v1.3.1

Version 1.3.1 corrects version reporting for CLIs installed with
`go install package@version`, aligns every from-scratch onboarding path on the
same module, library, CLI, verification, and initialization sequence, and adds
a CI contract that prevents those instructions from drifting again. Consult
the canonical changelog for complete release notes.

## v1.3.0

Version 1.3.0 adds the safe `confii init` bootstrapper and makes the complete
self-configuration template operational. It also enforces strict self-config
decoding, fixes declarative environment-variable override precedence, lets
`confii get` and `confii validate` use project defaults directly, and aligns
the built-in example and onboarding documentation with the generated named-file
layout. Consult the canonical changelog for the complete release notes.

## v1.2.1

Version 1.2.1 is a security and supply-chain assurance release. It adds
per-archive SPDX SBOMs, OpenVEX decisions, verified provenance, OpenBao
interoperability tests, Security Insights metadata, Fuzz Introspector reports,
and stricter protected CI, review, license, vulnerability, and release gates.
It also fixes the TOML NaN fuzz invariant and Dependabot DCO identity handling.
Consult the canonical changelog for the complete release notes.

## v1.2.0

Version 1.2.0 adds opt-in named environment-file discovery, explicit
environment strategy guardrails, hybrid conflict policies, runtime source-plan
introspection, and the `confii plan` command. Existing section-based and
explicit-loader configurations remain compatible. Consult the canonical
changelog for the complete release notes.

## v1.1.0

Version 1.1.0 is a defensive-hardening release focused on state isolation,
atomic rollback, concurrency safety, observable callback failures, strict input
normalization, independently versioned cloud modules, and reproducible release
automation. Consult the canonical changelog for the complete list of additions,
changes, fixes, and verification details.
