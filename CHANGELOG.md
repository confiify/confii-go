# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Core `Config[T]` with type-safe generics and fluent builder pattern
- Multi-source loading: YAML, JSON, TOML, INI, .env, environment variables, HTTP
- Cloud loaders: AWS S3, SSM, Azure Blob, GCS, IBM COS, Git repositories
- Secret management with `${secret:key}` placeholder resolution
- Cloud secret stores: AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, HashiCorp Vault (9 auth methods)
- 6 merge strategies (replace, merge, append, prepend, intersection, union) with per-path overrides
- Hydra-style config composition via `_include` and `_defaults` directives
- Environment resolution with automatic default + environment-specific merging
- 4-type hook system (key, value, condition, global) for value transformation
- Struct tag validation via go-playground/validator and JSON Schema validation
- Full introspection: Explain(), Layers(), Schema(), source tracking, override history
- Config diff, drift detection, versioning with rollback
- File watching with incremental reload (mtime + SHA256)
- Observability: access metrics, event emission
- Documentation generation (markdown, JSON)
- Export to JSON, YAML, TOML
- Self-configuration via `.confii.yaml` auto-discovery
- CLI tool with 10 commands: load, get, validate, export, diff, debug, explain, lint, docs, migrate
- 19 runnable examples
- GitHub Actions CI/CD: test matrix, CodeQL, govulncheck, OSSF Scorecard
- 96%+ test coverage
