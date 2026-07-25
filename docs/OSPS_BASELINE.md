# OpenSSF OSPS Baseline Evidence

**Assessment date:** 2026-07-25  
**Baseline version:** [2026.02.19](https://baseline.openssf.org/versions/2026-02-19.html)  
**Target:** Level 2, including every inherited Level 1 control

This is the project's control-to-evidence record for the Open Source Project
Security (OSPS) Baseline. It is a self-assessment backed by public source,
policy, release, and GitHub configuration evidence. A badge is displayed only
after the public OpenSSF Best Practices record accepts the same answers.

## Project scope

The released project consists of the root Go module and the separately tagged
`loader/cloud` and `secret/cloud` modules in this repository. They share one
release commit, workflow, security policy, and required checks. The
[`confiify/confii-go-examples`](https://github.com/confiify/confii-go-examples)
repository is a companion consumer application; it is not compiled into or
distributed with Confii release assets.

## Level 1 controls

| Control | Evidence |
| --- | --- |
| `OSPS-AC-01.01` | Every privileged account uses non-SMS 2FA; the dated maintainer assertion and continuity test are recorded in [`CONTINUITY.md`](CONTINUITY.md). |
| `OSPS-AC-02.01` | GitHub collaborator invitations require the administrator to select a role. The current access inventory is in [the maintainer record](https://github.com/confiify/confii-go/blob/main/MAINTAINERS.md). |
| `OSPS-AC-03.01`, `OSPS-AC-03.02` | The active `main-protection` ruleset requires pull requests and prevents branch deletion and non-fast-forward updates. |
| `OSPS-BR-01.01` | Workflows do not interpolate pull-request titles, branch names, commit messages, or other untrusted metadata into shell programs. Static matrices are single-quoted where passed to shell. |
| `OSPS-BR-01.03` | Untrusted pull-request code receives a read-only token, cannot access repository secrets from forks, and never runs through `pull_request_target` or a privileged follow-on workflow. |
| `OSPS-BR-03.01`, `OSPS-BR-03.02` | Official repository, documentation, module, issue, advisory, and release channels are HTTPS endpoints; releases also carry checksums and signed provenance. |
| `OSPS-BR-07.01` | GitHub secret scanning and push protection are enabled; required CI also scans the release tree with Gitleaks. |
| `OSPS-DO-01.01` | The [user documentation](https://confii-go-docs.pages.dev/) covers installation, basic and advanced library use, the CLI, examples, and integrations. |
| `OSPS-DO-02.01` | Public defect reporting is documented in [the support policy](https://github.com/confiify/confii-go/blob/main/SUPPORT.md) and contributor guidance; private security defects use [the security policy](https://github.com/confiify/confii-go/security/policy). |
| `OSPS-GV-02.01` | GitHub Issues and Discussions are the public proposal and usage-help channels. |
| `OSPS-GV-03.01` | [The contribution guide](https://github.com/confiify/confii-go/blob/main/CONTRIBUTING.md) defines setup, change, testing, licensing, sign-off, and review procedures. |
| `OSPS-LE-02.01`, `OSPS-LE-02.02` | Source and released assets use the OSI-approved MIT License. |
| `OSPS-LE-03.01`, `OSPS-LE-03.02` | `LICENSE`, `LICENSES/MIT.txt`, and REUSE metadata are versioned; GoReleaser includes `LICENSE` in every release archive. |
| `OSPS-QA-01.01`, `OSPS-QA-01.02` | The public Git repository is the authoritative, immutable history of changes, authors, signatures, reviews, and dates. |
| `OSPS-QA-02.01` | Every Go module has `go.mod` and `go.sum`; documentation and compliance Python dependencies use hash-locked requirements. |
| `OSPS-QA-04.01` | The complete released-code and companion-repository inventory is documented in **Project scope** above. |
| `OSPS-QA-05.01`, `OSPS-QA-05.02` | No generated executable, library, archive, or other unreviewable binary is tracked. Tracked PNG branding is an allowed graphical asset and its SVG counterpart is reviewable source. |
| `OSPS-VM-02.01` | [The security policy](https://github.com/confiify/confii-go/security/policy) publishes the private advisory URL and security email contact. |

## Level 2 controls

| Control | Evidence |
| --- | --- |
| `OSPS-AC-04.01` | Repository Actions default workflow permissions are read-only; checked-in workflows explicitly elevate only release and security-upload jobs. |
| `OSPS-BR-02.01` | Root and nested modules use immutable Semantic Version tags tied to the same commit. |
| `OSPS-BR-04.01` | `CHANGELOG.md`, GitHub-native release notes, and the release procedure record functional and security changes. |
| `OSPS-BR-05.01` | Builds use standard Go modules and a version-pinned GoReleaser pipeline. Python tools use hash-locked pip requirements. |
| `OSPS-BR-06.01` | Releases contain SHA-256 checksums and a Sigstore-backed GitHub artifact-attestation bundle covering every checksummed asset. |
| `OSPS-DO-06.01` | Dependency selection, tracking, updates, vulnerability review, and suppression rules are defined in [`QUALITY.md`](QUALITY.md) and [the security policy](https://github.com/confiify/confii-go/security/policy). |
| `OSPS-DO-07.01` | [The contribution guide](https://github.com/confiify/confii-go/blob/main/CONTRIBUTING.md), the Makefile, module manifests, and [`RELEASING.md`](RELEASING.md) provide reproducible setup and build instructions. |
| `OSPS-GV-01.01`, `OSPS-GV-01.02` | [The maintainer record](https://github.com/confiify/confii-go/blob/main/MAINTAINERS.md) lists every privileged member, access level, role, and responsibility. |
| `OSPS-GV-03.02` | Contributor acceptance requirements cover tests, style, API/docs impact, licensing metadata, DCO, and independent review. |
| `OSPS-LE-01.01` | The required `DCO sign-off` CI job validates the full proposed commit range and requires a matching author sign-off on every commit. |
| `OSPS-QA-03.01` | Protected `main` requires up-to-date `CI required`, Govulncheck, CodeQL, signature, review, and conversation-resolution gates; bypasses remain visible in GitHub history. |
| `OSPS-QA-06.01` | Required CI runs unit, integration, race, cloud-consumer, fuzz, coverage, lint, vulnerability, dependency, secret, and license tests before merge. |
| `OSPS-SA-01.01`, `OSPS-SA-02.01` | [`ARCHITECTURE.md`](ARCHITECTURE.md), Go Reference, feature guides, and CLI documentation describe actors, data flows, extension interfaces, public APIs, and commands. |
| `OSPS-SA-03.01` | The dated [`SECURITY_REVIEW.md`](SECURITY_REVIEW.md) assessment records boundary, requirements, method, findings, mitigations, and residual risks. |
| `OSPS-VM-01.01`, `OSPS-VM-03.01` | [The security policy](https://github.com/confiify/confii-go/security/policy) defines coordinated disclosure, response/remediation targets, GitHub private reporting, and a private fallback address. |
| `OSPS-VM-04.01` | Confirmed vulnerabilities are published as GitHub Security Advisories after coordination and referenced in the changelog or release notes. |

## Verification and maintenance

The maintainer audit also confirmed on 2026-07-25 that Actions defaults to
read-only, secret scanning and push protection are enabled, and active rulesets
protect `main` and version tags. Reassess this record after changes to access,
repository rules, workflows, release construction, project scope, or the OSPS
Baseline version. A control is changed to **Unmet** or **Unknown** when its
evidence no longer holds; badge percentages are not a reason to retain a stale
claim.
