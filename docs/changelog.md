# Changelog

The canonical, release-by-release changelog is maintained in
[CHANGELOG.md](https://github.com/confiify/confii-go/blob/main/CHANGELOG.md).

## Upcoming release

Version 2.4.0 makes Confii an authority over the secrets it resolves. Vault
providers can be built hermetically, so no ambient environment variable can
weaken transport security; strict declarative configuration is closed over
setting names, value types, and meaning, so a declaration either takes effect
or is refused rather than being accepted and ignored. `RedactedDict` and
`ExportRedacted` give a safe projection of a configuration, secret references
are judged before any provider is contacted, and `secret.Reference` now upholds
its own serialization contract. Consult the canonical changelog for complete
release notes.

## v2.4.0

Version 2.4.0 adds hermetic Vault construction, strict provider configuration
that is closed over names, types, and meaning, a redacted projection of a
configuration, and admission of secret-reference syntax before any provider is
contacted. `secret.Reference` gains `Validate`, `MarshalText`, and
`UnmarshalText`, and `GenerateDocs` now honours declared sensitive paths. See
[Secrets](secrets.md), [Ownership](ownership.md), and the canonical changelog
for details.

## v2.3.0

Version 2.3.0 adds `PositionalLoader`, so YAML-sourced keys report the exact
line they came from, and corrects override counting so only real value changes
register as conflicts. See [Introspection](introspection.md) and the canonical
changelog for details.

## v2.2.0

Version 2.2.0 adds opt-in strict typed decoding through
`WithRejectUnknownKeys` and the new `validate.Options` entry points, and
corrects the typed decode path so it honors `WithTypeCasting`. See
[Validation](validation.md) and the canonical changelog for details.

## v2.1.0

Version 2.1.0 introduces the resolver system and onboarding documentation
enhancements after the v2.0.0 lifecycle release. See
[Custom Value Resolvers](extensibility.md#custom-value-resolvers) and the
canonical changelog for details.

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
