# Security metadata and assurance tooling

Confii combines runtime tests with machine-readable evidence. These
integrations are release controls or monitoring aids; none is presented as a
substitute for maintainer review.

## Security Insights

The root [`security-insights.yml`](https://github.com/confiify/confii-go/blob/main/security-insights.yml)
publishes the project's security contacts, policies, repositories, release
artifacts, assessment evidence, and security-tool integrations using OpenSSF
Security Insights schema 2.2.0. `make security-insights-check` parses it with
the pinned official `ossf/si-tooling` Go module and enforces Confii-specific
vulnerability-reporting invariants.

Review and update the dates and claims whenever the security posture,
maintainers, release process, or supporting evidence changes.

## Native fuzzing and Fuzz Introspector

The required CI fuzz matrix runs every native Go fuzz target for 30 seconds.
The separate Fuzz Introspector job performs static call-graph and complexity
analysis with the official tree-sitter Go frontend, checks that every source
fuzz target appears in both the Makefile and CI inventories, and uploads the
complete HTML report as the `fuzz-introspector-report` workflow artifact for 14
days.

Fuzz Introspector 0.1.10 currently depends on Atheris wheels available for
Linux x86-64, so report generation uses its supported Python 3.11/Linux
environment. Native Go fuzzing remains portable and authoritative for actually
executing inputs. Python dependencies are fully hash-locked under
`tools/fuzz-introspector/`. Fuzz Introspector 0.1.10 exact-pins vulnerable
`lxml` and `soupsieve` versions in its wheel metadata; CI installs the official
wheel without dependencies and supplies tested, fixed versions from the
separate hash lock. OSV-Scanner verifies that lock on every security run.

## OpenBao

The `OpenBao interoperability` CI job starts OpenBao 2.6.1 from a digest-pinned
official image. It provisions a short-lived AppRole and runs Confii through the
same temporary consumer-module shape used by the cloud tests. The live test
covers token and AppRole authentication plus KV v2 write, read, field
extraction, list, and delete operations. See the secrets guide for the
`cloud.NewOpenBao` API.

This is a versioned compatibility statement, not a claim that every future
OpenBao version or every external authentication provider has been certified.

## Minder

The audit-only Minder profile and Custcodian hosted-service activation process
are documented in the [Minder guide](MINDER.md). The repository profile does
not mean hosted monitoring is active until a maintainer enrolls the repository
and applies it.

## SBOM validation with bomctl

GoReleaser and Syft create one SPDX 2.3 JSON SBOM for each compiled release
archive. `make release-artifacts-check` installs bomctl from an immutable
upstream commit, imports every SBOM into bomctl's protobom-backed store,
re-exports it as SPDX 2.3, and validates the resulting document. Checksums,
archive count, and OpenVEX inclusion are checked in the same gate.

Because bomctl currently describes itself as experimental, Confii uses it as a
second semantic interoperability check while retaining direct SPDX structural
validation and checksums.
