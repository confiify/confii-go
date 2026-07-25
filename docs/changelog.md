# Changelog

The canonical, release-by-release changelog is maintained in
[CHANGELOG.md](https://github.com/confiify/confii-go/blob/main/CHANGELOG.md).

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
