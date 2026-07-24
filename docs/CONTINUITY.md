# Maintainer continuity

This runbook defines how Confii keeps issue handling, change acceptance, and
releases available if any one maintainer becomes unavailable. It complements
the routine governance and release procedures; it does not replace review,
testing, signature, or provenance requirements.

## Activation status

Repository access and emergency protections are configured for `qualityCOE`,
the continuity maintainer. Final activation requires the remaining release dry
run and recorded drill below. Until those checks complete, the project continues
to report the OpenSSF `access_continuity` and `bus_factor` criteria as unmet.

## People and access

The public maintainer roster is in
[`MAINTAINERS.md`](https://github.com/confiify/confii-go/blob/main/MAINTAINERS.md).
Each maintainer uses an individual GitHub account, individual authentication
and signing keys, and non-SMS two-factor authentication. Private keys,
passwords, recovery codes, and one-time-password seeds are never shared.

The continuity maintainer needs only the repository capabilities required to:

- create, triage, and close issues;
- create and review pull requests and resolve review threads;
- push branches and signed tags;
- merge an emergency pull request after required CI succeeds; and
- run and verify the release process in [`RELEASING.md`](RELEASING.md).

Routine changes still require independent review. A narrowly scoped ruleset
bypass may be assigned to the continuity maintainer so the loss of the other
maintainer cannot deadlock the repository. It is used only under the emergency
procedure below and leaves a public pull-request and audit trail.

## Readiness checklist

The lead maintainer verifies the following before declaring this arrangement
active and after any relevant access change:

- [x] the candidate accepted repository collaborator access;
- [x] GitHub recognizes the candidate's commit and tag signing key;
- [x] the candidate confirmed non-SMS two-factor authentication is enabled;
- [x] branch and release-tag rules permit the documented emergency path;
- [ ] the candidate can inspect Actions results and repository security alerts
      needed to evaluate a change;
- [ ] the candidate completed the release procedure from a clean checkout;
- [ ] a continuity drill was completed without sharing credentials; and
- [ ] the drill result and evidence links were reviewed publicly.

## Emergency procedure

The continuity path may be activated after the lead maintainer explicitly
delegates it, or after credible confirmation that the lead maintainer is unable
or unwilling to support the project. A short transient absence is not an
emergency.

Within seven days of confirmation, the continuity maintainer:

1. records the activation and its non-sensitive basis in a public issue;
2. triages urgent issues and security reports without disclosing confidential
   vulnerability information;
3. prepares changes as signed commits on a dedicated branch and opens a pull
   request;
4. waits for every required CI, vulnerability, static-analysis, and CodeQL gate;
5. obtains independent review when another maintainer is available;
6. uses the documented emergency bypass only when loss of that maintainer would
   otherwise deadlock the repository; and
7. follows `docs/RELEASING.md` for any release, including signed immutable tags,
   GitHub attestations, and post-release consumer verification.

Emergency access does not permit rewriting protected history, moving published
tags, suppressing failing checks, sharing credentials, or silently changing
governance. Any bypass is documented in the pull request and reviewed
retrospectively when another maintainer is available.

## Drill record

The drill uses a harmless documentation-only pull request and the manual
`Maintainer continuity drill` workflow. The workflow enters the protected
`release` environment, runs module and race tests, and creates a GoReleaser
snapshot without publishing a release.

It verifies the candidate's own checkout, authentication, signature, issue and
pull-request permissions, CI visibility, protected-branch path, protected-tag
path, and release commands. It must not create a misleading production release
or expose keys and recovery material.

| Date | Maintainer | Result | Public evidence |
| --- | --- | --- | --- |
| Pending | `qualityCOE` | Not run | [Tracking issue #60](https://github.com/confiify/confii-go/issues/60) |

The readiness checklist and drill are repeated at least annually, after a
maintainer or protection change, and before claiming continuity evidence that
has not previously been exercised.

## Known boundary

Confii is hosted in a GitHub personal-account repository. Repository continuity
does not grant the continuity maintainer ownership of the hosting account or
access to the lead maintainer's personal credentials. If a future operational
dependency cannot be delegated through repository permissions, its recovery
and any necessary legal authority must be placed in an appropriate private
escrow; only the existence and successful test of that arrangement are recorded
publicly.
