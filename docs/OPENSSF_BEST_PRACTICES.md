# OpenSSF Best Practices Evidence

Confii is pursuing the OpenSSF Best Practices Gold badge under
[project 12279](https://www.bestpractices.dev/projects/12279). The badge is a
human-reviewed self-certification, not an automated score. Claims must be
supported by repository evidence and actual project practice.

## Machine-readable proposals

The repository-root `.bestpractices.json` uses the badge service's supported
automation format. It proposes statuses and justifications for criteria that
can be substantiated by public code, policy, workflow, release, or hosting
evidence. Unknown values are deliberately omitted rather than guessed.

After the file reaches `main`, an authorized badge maintainer must open each
Passing, Silver, and Gold edit form, select **Save and continue** to rerun
automation, inspect the highlighted proposals, and save only accurate answers.
Achieving Passing unlocks Silver; achieving Silver unlocks Gold.

## Evidence added in the 2026 review

- public governance, maintainer responsibilities, support targets, and roadmap;
- a consolidated coding, testing, coverage, dependency, and review policy;
- a dated security boundary review and assurance case;
- required static analysis, vulnerability analysis, fuzzing, race detection,
  integration, cloud-consumer, secret-scanning, and dependency-review gates;
- byte-for-byte reproducible CLI build verification;
- an enforced 90% statement-coverage threshold across non-example shipping
  code, including the CLI;
- an enforced 80% condition/branch-coverage threshold using pinned FLOSS
  tooling across the same shipping-code scope;
- signed tags, verified commits, checksums, and release provenance;
- copyright and SPDX license statements in source and build automation; and
- public, bounded `good first issue` tasks for new contributors.

## Human continuity evidence

The project has two independently controlled maintainers with individual
authentication, signing keys, and non-SMS 2FA. The continuity maintainer has
write access, a pull-request-only emergency branch bypass, protected tag
authority, and release-environment review access. The procedure and successful
2026-07-25 drill are recorded in [`CONTINUITY.md`](CONTINUITY.md).

This evidence supports the OpenSSF `access_continuity` and `bus_factor`
criteria. It does not by itself establish the separate contribution-history or
review-ratio criteria below.

## Gold readiness snapshot

The following criteria depend on people, access, authentication methods, or
sustained history. Repository text alone cannot make them true.

| Criterion | Current evidence | Requirement before it can be marked Met |
| --- | --- | --- |
| `contributors_unassociated` | The public record does not yet establish two qualifying contributors. | At least two significant contributors are unaffiliated under the badge definition. |
| `two_person_review` | The 2026-07-25 ledger records 5 qualifying independently reviewed PRs among 19 non-trivial merged PRs. | Public history demonstrates that another person reviewed at least 50% of non-trivial changes before acceptance. |
| `secure_2FA` | **Met:** the people controlling `confiify`, `qatoolist`, and `qualityCOE` confirmed on 2026-07-25 that every privileged account uses an authenticator application and none relies on SMS-only 2FA. | Reconfirm after any privileged-account or authentication-method change. This is a Gold recommendation, not a mandatory Gold criterion. |
| `hardened_site` | **Met:** <https://confii-go-docs.pages.dev/> passed `make docs-live-headers` on 2026-07-25 with nonpermissive CSP, MIME-sniffing protection, anti-framing, HSTS, referrer, and permissions policies. Deployment and repeatable-test evidence is in [`HOSTING.md`](HOSTING.md). | Repeat the live test and browser smoke test after any hosting or header-policy change. |

Confii will not mark these criteria Met until the public or maintainer-verified
evidence supports them. This distinction keeps the badge credible.

## Independent-review baseline

The baseline counts merged, non-trivial pull requests rather than commits or
accounts. A review qualifies only when it is submitted before merge by a
different person who evaluated the final material revision. Automated reviews,
self-review through another account, and post-merge comments do not qualify.

PRs #15 and #17 are excluded as test-only administrative changes. The current
ledger is:

| PR | Author | Independent reviewer | Reviewed final head | Review before merge |
| --- | --- | --- | --- | --- |
| [#63](https://github.com/confiify/confii-go/pull/63) | `confiify` | `qualityCOE` | `cf379fe0ba4ff57843771b8abe41f4229f865aa7` | 2026-07-24 20:47:23 UTC; merged 20:47:58 UTC |
| [#65](https://github.com/confiify/confii-go/pull/65) | `confiify` | `qualityCOE` | `ec1f15132333d81ae901c36f7d5f8b648a8fff6c` | 2026-07-24 21:42:12 UTC; merged 21:49:57 UTC |
| [#66](https://github.com/confiify/confii-go/pull/66) | `confiify` | `qualityCOE` | `32667d45e0d3bfa007ec51625e025d10480964df` | 2026-07-24 21:54:31 UTC; merged 21:56:49 UTC |
| [#67](https://github.com/confiify/confii-go/pull/67) | `confiify` | `qualityCOE` | `bbb0c22a6f34fb9a7159013b8c98c79c8b98ddd5` | 2026-07-24 22:00:46 UTC; merged 22:03:11 UTC |
| [#68](https://github.com/confiify/confii-go/pull/68) | `confiify` | `qualityCOE` | `c6223a08a1d36e2d8436159ed7510724c54f569f` | 2026-07-24 22:22:01 UTC; merged 22:24:54 UTC |

This establishes 5 qualifying reviews among 19 non-trivial merged PRs. If
every subsequent PR qualifies, 9 additional substantive reviewed PRs are
needed to reach 50%: `(5 + 9) / (19 + 9) = 50%`. This is a planning baseline,
not a request to create artificial pull requests. The ratio is recalculated
from the public history at each release.

The default CODEOWNERS set includes the independently controlled lead and
continuity maintainers. Repository rules must require code-owner approval and
dismiss stale approvals so the author cannot satisfy routine review alone.

## Work allocation

Repository-controlled work includes the review policy, PR evidence fields,
static-site security policy, build validation, public roadmap, and honest badge
proposals. Maintainer-controlled work includes confirming account 2FA methods,
enabling the matching repository rule, provisioning hardened hosting with
continuity access, and inviting genuine unaffiliated participation.

## Review cadence

Evidence is rechecked during every release and after changes to maintainers,
repository protection, CI, release automation, security boundaries, or hosting.
Broken evidence is corrected in the repository and the badge record rather
than left as a stale claim.
