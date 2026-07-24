# Quality and Engineering Policy

This document defines the controls used to build, test, review, and release
Confii. The executable source of truth is the repository's `Makefile`, GitHub
Actions workflows, and protected-branch rules.

## Coding and review standards

- Go code follows `gofmt`, `go vet`, the Go Code Review Comments, and Effective
  Go. Exported identifiers require doc comments.
- `golangci-lint`, CodeQL, dependency review, Govulncheck, OSV Scanner, and
  Gitleaks findings must be resolved or accompanied by a narrowly scoped,
  documented rationale.
- Pull requests must be focused, explain user and security impact, update
  documentation where behavior changes, and include a DCO sign-off on every
  commit.
- A code-owner other than the last pusher must approve changes. Stale approvals
  are dismissed, conversations must be resolved, required checks must pass,
  and commits reaching `main` must have verified signatures.
- Reviewers check API compatibility, failure behavior, input validation,
  concurrency safety, secret handling, dependency impact, documentation, and
  tests—not only formatting or the happy path.

## Test policy

Every new behavior must have positive, negative, and boundary tests. Every bug
fix must add a regression test that fails without the fix unless the pull
request documents why an automated regression test is technically impossible.
At least half of new non-trivial functionality must be accompanied by tests;
Confii's stricter policy requires tests for all new functionality.

The required suite includes:

- unit and integration tests;
- race-detector tests on supported non-Windows platforms;
- isolated consumer tests for every cloud-provider build tag;
- short fuzz campaigns for parsers, merging, normalization, and secret
  resolution;
- static analysis, vulnerability analysis, dependency review, and secret
  scanning; and
- documentation builds with warnings treated as errors.

Run `make ci-full`, `make lint`, `make vulncheck`, and `make docs-check` before
requesting review. CI runs the same classes of checks on every pull request.

## Coverage

The project requires at least 90% aggregate statement coverage across all
non-example shipping Go code, including the CLI, and 90% Codecov patch
coverage. Runnable documentation examples and the integration harness are
excluded from the statement denominator; their behavior is exercised by build
and integration jobs. `scripts/check-statement-coverage.sh` computes and
enforces the aggregate threshold in CI. Coverage must not drop below the
threshold to merge.

Go's standard coverage tooling measures statement coverage, not branch
coverage. The Gold branch-coverage criterion is therefore not applicable until
a maintained FLOSS tool can reliably measure Go branch coverage. Negative,
table-driven, integration, fuzz, and race tests provide complementary path
coverage in the meantime.

## Builds and reproducibility

`make build` uses the standard Go build system and honors normal Go environment
variables such as `GOOS`, `GOARCH`, `GOFLAGS`, `GOMODCACHE`, and `GOCACHE`.
Dependencies and checksums are declared in each module's `go.mod` and `go.sum`.
`make mod-verify` independently verifies every module.

`make reproducible-build-check` builds the CLI twice from the same source and
declared inputs in separate clean output directories using `-trimpath`, a fixed
version string, and disabled VCS stamping, then requires byte-for-byte equality.
CI runs this check on every proposed change. Release archives also use a fixed
source commit timestamp and a pinned GoReleaser version.

## Dependency lifecycle

Go module manifests are the dependency inventory. Dependabot proposes updates;
dependency review rejects newly introduced vulnerable or incompatible
dependencies; Govulncheck checks reachable Go vulnerabilities; and OSV Scanner
checks every nested module and documentation lockfile. Confirmed vulnerabilities
are prioritized according to `SECURITY.md` and are fixed by updating, removing,
or isolating the affected component.

## Release and maintenance policy

Releases follow Semantic Versioning and the multi-module procedure in
`docs/RELEASING.md`. Tags are immutable and signed, release artifacts carry
GitHub attestations, and supported release lines are listed in `SECURITY.md`.
Older unsupported versions remain available from Git history and releases.
