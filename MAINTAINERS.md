# Maintainers

| GitHub account | Repository access | Role |
| --- | --- | --- |
| [@confiify](https://github.com/confiify) | Admin | Lead maintainer; releases, security response, repository administration, roadmap decisions |
| [@qatoolist](https://github.com/qatoolist) | Write | Reviewing maintainer; approval, quality gates, release-readiness review |
| [@qualityCOE](https://github.com/qualityCOE) | Write; protected-tag and release-continuity authority | Continuity maintainer; issue and pull-request operations, independent review, and emergency releases |

This table is the public inventory of people with access to repository-sensitive
resources. GitHub Actions defaults to read-only permissions; individual
workflows elevate only the jobs documented in their checked-in configuration.

For general help, use the channels in [SUPPORT.md](SUPPORT.md). Report
vulnerabilities through the private process in [SECURITY.md](SECURITY.md), not
through a public issue or direct message.

No maintainer may approve their own last push. Changes to maintainer access or
responsibilities are made through a reviewed pull request under
[GOVERNANCE.md](GOVERNANCE.md). Release preparation includes confirming that a
second maintainer can review the source, required checks, and release evidence;
the project does not claim a bus factor of two unless that continuity exists in
practice. The activation and verification procedure is in
[`docs/CONTINUITY.md`](docs/CONTINUITY.md).
