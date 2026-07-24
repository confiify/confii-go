# Governance

Confii uses a maintainer-led governance model. The current roles and contact
points are listed in [MAINTAINERS.md](MAINTAINERS.md).

Maintainer responsibilities are separated between release/security ownership
and review. At least two maintainers should retain sufficient
repository knowledge and access to review, release, respond to vulnerabilities,
and recover the project. `MAINTAINERS.md` is the public role record; actual
access is reviewed after each maintainer change and during release preparation.

## Decisions

Routine changes are proposed through pull requests and decided by maintainers
after public review. The protected default branch requires passing checks, a
verified commit signature, and approval from a code owner other than the last
pusher. Maintainers aim for consensus and record material design decisions in
the relevant issue or pull request.

For routine changes, "independent review" means review by a different person,
not merely a second account controlled by the author. The reviewer evaluates
the final material revision before merge. A later comment, an automated check,
or approval of a revision that was materially replaced is not counted as
independent acceptance. Release evidence records the qualifying review history
without treating emergency continuity bypasses as routine reviews.

When consensus cannot be reached, the lead maintainer makes the final decision
after considering the technical evidence, user impact, compatibility, security,
and long-term maintenance cost. Anyone may ask for reconsideration by opening a
focused issue with new evidence.

Security-sensitive decisions follow the private disclosure process in
[SECURITY.md](SECURITY.md) until coordinated disclosure is safe.

## Maintainer continuity

The project maintains a tested continuity path under
[`docs/CONTINUITY.md`](docs/CONTINUITY.md). Routine work never uses emergency
access. If loss of a maintainer would otherwise deadlock a protected branch or
release, the continuity maintainer may use the scoped bypass recorded in the
repository rules only after required automated checks succeed. The pull request
must identify the activation, explain why independent review was unavailable,
and receive retrospective review when another maintainer is available.

Emergency access does not authorize rewriting history, moving release tags,
publishing unreviewed source, or disabling security gates. A release performed
under continuity procedures retains the signed-tag, immutable-release,
attestation, and consumer-verification requirements in `docs/RELEASING.md`.

## Maintainer changes

Maintainers are selected based on sustained, constructive contributions;
sound technical judgment; reliable review; and adherence to the Code of
Conduct. The lead maintainer grants or removes repository access and records
role changes in `MAINTAINERS.md` through a reviewed pull request.

A maintainer may step down at any time. Access may also be removed for extended
inactivity, repeated failure to protect users or repository credentials, or a
confirmed Code of Conduct violation. Except for urgent security containment,
the affected maintainer is given notice and an opportunity to respond.

## Changing this policy

Governance changes use the same reviewed pull-request process as code changes.
The pull request must explain the motivation, migration impact, and any change
to contributor or maintainer responsibilities.
