# Contributing to Confii

Thank you for your interest in contributing to Confii! This guide will help you get started.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<your-username>/confii-go.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Run checks: `make check`
6. Push and open a pull request

## Development Setup

```bash
go mod download
make deps
```

## Running Checks

Before submitting a PR, run:

```bash
make check       # fmt + vet + test
make lint         # fmt-check + vet + golangci-lint
make vulncheck    # govulncheck
make test-race    # tests with race detector
make test-cover   # coverage report
```

All checks must pass. Target 90%+ test coverage for new code.

## Code Style

- Follow standard Go conventions and [Effective Go](https://go.dev/doc/effective-go)
- All exported types, functions, and methods must have doc comments starting with the name
- Run `gofmt -s` before committing (or `make fmt`)
- No `golangci-lint` warnings allowed

## Pull Request Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if behavior changes
- Reference related issues in the PR description

## Reporting Issues

- Use the bug report template for bugs
- Use the feature request template for new features
- Search existing issues before creating a new one

## Code of Conduct

This project follows our [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you agree to uphold it.
