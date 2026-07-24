# Governance

Confii uses a maintainer-led governance model. The current roles and contact
points are listed in [MAINTAINERS.md](MAINTAINERS.md).

Maintainer responsibilities are separated between release/security ownership
and independent code review. At least two maintainers should retain sufficient
repository knowledge and access to review, release, respond to vulnerabilities,
and recover the project. `MAINTAINERS.md` is the public role record; actual
access is reviewed after each maintainer change and during release preparation.

## Decisions

Routine changes are proposed through pull requests and decided by maintainers
after public review. The protected default branch requires passing checks, a
verified commit signature, and approval from a code owner other than the last
pusher. Maintainers aim for consensus and record material design decisions in
the relevant issue or pull request.

When consensus cannot be reached, the lead maintainer makes the final decision
after considering the technical evidence, user impact, compatibility, security,
and long-term maintenance cost. Anyone may ask for reconsideration by opening a
focused issue with new evidence.

Security-sensitive decisions follow the private disclosure process in
[SECURITY.md](SECURITY.md) until coordinated disclosure is safe.

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
