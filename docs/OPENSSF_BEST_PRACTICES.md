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
- signed tags, verified commits, checksums, and release provenance;
- copyright and SPDX license statements in source and build automation; and
- public, bounded `good first issue` tasks for new contributors.

## Criteria not yet claimed

The following criteria depend on people, access, authentication methods, or
sustained history. Repository text alone cannot make them true.

| Criterion | Requirement before it can be marked Met |
| --- | --- |
| `access_continuity` | A second real maintainer has the knowledge and effective access needed to continue the project. |
| `bus_factor` | At least two people—not merely two accounts controlled by one person—can maintain and release the project. |
| `contributors_unassociated` | At least two significant contributors are unaffiliated under the badge definition. |
| `two_person_review` | Public history demonstrates that another person reviewed at least 50% of non-trivial changes before acceptance. |
| `secure_2FA` | Privileged maintainers confirm they use phishing-resistant or app/hardware-based 2FA rather than SMS-only authentication. This is a Gold recommendation, not a mandatory Gold criterion. |

Confii will not mark these criteria Met until the public or maintainer-verified
evidence supports them. This distinction keeps the badge credible.

## Review cadence

Evidence is rechecked during every release and after changes to maintainers,
repository protection, CI, release automation, security boundaries, or hosting.
Broken evidence is corrected in the repository and the badge record rather
than left as a stale claim.
