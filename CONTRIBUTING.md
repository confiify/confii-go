# Contributing to Confii

Thank you for your interest in contributing to Confii! This guide will help you get started.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<your-username>/confii-go.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Sign off every commit: `git commit --signoff`
6. Run checks: `make ci-full`
7. Push and open a pull request

## Developer Certificate of Origin

Confii uses the [Developer Certificate of Origin 1.1](https://developercertificate.org/)
to confirm that contributors have the right to submit their work under this
project's license. Every commit must contain a `Signed-off-by` trailer matching
the contributor's commit identity:

```text
Signed-off-by: Your Name <your.email@example.com>
```

Create it with `git commit --signoff` (or `git commit -s`). This sign-off is a
legal certification, not a replacement for the cryptographic commit signatures
required on the protected default branch. If a pull request contains an
unsigned commit, amend or rebase it to add the trailer before review.

## Development Setup

```bash
make deps
```

## Running Checks

Before submitting a PR, run:

```bash
make mod-verify   # every module is tidy and checksummed
make dco-check    # DCO sign-offs on commits not yet in origin/main
make lint         # fmt-check + vet + golangci-lint
make reuse-lint   # machine-readable copyright and license coverage
make ci-full      # core, race, integration, and cloud consumer tests
make vulncheck    # govulncheck for the core module
make docs-check
```

All checks must pass. CI independently checks every pull-request commit and
rejects a sign-off that is missing or does not match that commit's author
identity. Target 90%+ test coverage for new code.

## Code Style

- Follow standard Go conventions and [Effective Go](https://go.dev/doc/effective-go)
- All exported types, functions, and methods must have doc comments starting with the name
- Run `gofmt -s` before committing (or `make fmt`)
- No `golangci-lint` warnings allowed

## Licensing metadata

Confii follows the REUSE Specification 3.3. Repository-authored material is
licensed under MIT and must retain its embedded SPDX header or be covered by
`REUSE.toml`. Run `make reuse-lint` before submitting changes.

Do not place third-party material under the repository-wide MIT fallback. A
contribution that imports or adapts third-party material must preserve its
copyright and license information, add the corresponding canonical license
text under `LICENSES/`, and add a narrower `REUSE.toml` annotation where
needed. Reviewers must be able to trace the origin and redistribution terms.

## Testing Policy

All new functionality **must** include tests. This is enforced through:

- **Coverage target:** New code must maintain 90%+ test coverage. The CI pipeline reports coverage via Codecov on every PR.
- **Test with PR:** Every pull request must include tests that cover the new or changed functionality. PRs without tests for new features will not be merged.
- **Regression tests:** Every bug fix must include a test that fails without the fix unless the pull request documents why automation is technically impossible.
- **Race detection:** Tests are run with `-race` in CI to catch concurrency issues.
- **Linting:** `golangci-lint` enforces error checking (`errcheck`), unused code detection, and other quality rules.

Run the full test suite locally before submitting:

```bash
make test          # unit tests
make test-race     # with race detector
make test-cover    # with coverage report
make test-cloud    # all cloud-provider tags in a consumer fixture
```

## Pull Request Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if behavior changes
- Reference related issues in the PR description
- Ensure every commit includes a DCO `Signed-off-by` trailer
- Follow the security and quality review checklist in [docs/QUALITY.md](docs/QUALITY.md)
- Leave final acceptance to a reviewer who is a different person from the
  author and who has reviewed the final material revision

## Contributor pathway

Confii welcomes contributors who are not maintainers and are not affiliated
with the project's maintainers. Issues labelled
[`good first issue`](https://github.com/confiify/confii-go/issues?q=is%3Aopen+label%3A%22good+first+issue%22)
are intended to be bounded entry points; help is available through the public
channels in [SUPPORT.md](SUPPORT.md).

Maintainer status is not required for contribution credit. Meaningful code,
tests, documentation, design, security, and interoperability contributions are
credited through Git history, pull requests, release notes where appropriate,
and the GitHub contributors view. Contributions are evaluated for user value
and technical quality—not to manufacture badge statistics.

## Reporting Issues

- Use the bug report template for bugs
- Use the feature request template for new features
- Search existing issues before creating a new one

## Code of Conduct

This project follows our [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you agree to uphold it.
