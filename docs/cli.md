# CLI Tool

Confii includes a command-line tool with 10 commands for loading, inspecting, validating, exporting, and comparing configurations.

---

## Installation

```bash
go install github.com/confiify/confii-go/confii@latest
```

Verify:

```bash
confii --help
```

---

## Loader Syntax

Most commands accept one or more `--loader` (or `-l`) flags in the format `type:source`:

| Loader Type | Syntax | Example |
|-------------|--------|---------|
| YAML | `yaml:path` | `-l yaml:config.yaml` |
| JSON | `json:path` | `-l json:config.json` |
| TOML | `toml:path` | `-l toml:config.toml` |
| INI | `ini:path` | `-l ini:config.ini` |
| .env file | `env_file:path` | `-l env_file:.env` |
| Environment vars | `env:PREFIX` | `-l env:APP` |
| HTTP | `http:url` | `-l http:https://example.com/config.json` |

You can pass multiple loaders. Later loaders override earlier ones with deep merge.

---

## Commands

### load

Load and display the merged configuration as JSON.

```bash
confii load [env] -l type:source [...]
```

**Examples:**

```bash
# Load with environment
confii load production -l yaml:config.yaml

# Multiple sources
confii load production -l yaml:base.yaml -l yaml:prod.yaml

# No environment
confii load -l yaml:config.yaml -l env:APP
```

---

### get

Retrieve a single configuration value by key path.

```bash
confii get <env> <key> -l type:source [...]
```

**Examples:**

```bash
# Get a scalar value
confii get production database.host -l yaml:config.yaml
# Output: prod-db.example.com

# Get a nested object (printed as indented JSON)
confii get production database -l yaml:config.yaml
# Output:
# {
#   "host": "prod-db.example.com",
#   "port": 5432
# }
```

---

### validate

Validate configuration against a JSON Schema file.

```bash
confii validate [env] -l type:source --schema schema.json
```

**Flags:**

| Flag | Description | Required |
|------|-------------|----------|
| `--schema` | Path to JSON Schema file | Yes |

**Examples:**

```bash
confii validate production -l yaml:config.yaml --schema schema.json
# Output: Configuration is valid.

# Fails with non-zero exit code if invalid
confii validate production -l yaml:config.yaml --schema strict-schema.json
# Output: Validation failed: ...
```

---

### export

Export configuration in a different format.

```bash
confii export [env] -l type:source -f format [-o output]
```

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `-f, --format` | Output format (`json`, `yaml`) | `json` |
| `-o, --output` | Output file path | stdout |

**Examples:**

```bash
# Export as JSON to stdout
confii export production -l yaml:config.yaml -f json

# Export as YAML to file
confii export production -l yaml:config.yaml -f yaml -o output.yaml

# Convert TOML to JSON
confii export -l toml:config.toml -f json -o config.json
```

---

### diff

Compare two configurations (different environments or different sources).

```bash
confii diff <env1> <env2> --loader1 type:source --loader2 type:source [-f format]
```

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--loader1` | Loaders for the first config | Required |
| `--loader2` | Loaders for the second config | Same as `--loader1` |
| `-f, --format` | Output format (`unified`, `json`) | `unified` |

**Examples:**

```bash
# Compare two environments using the same source file
confii diff dev production --loader1 yaml:config.yaml --loader2 yaml:config.yaml

# Output:
# ~ database:
#   ~ host: localhost -> prod-db.example.com
# + monitoring.enabled = true
#
# Summary: 2 changes (1 added, 0 removed, 1 modified)

# JSON output for CI
confii diff dev production --loader1 yaml:config.yaml -f json
```

!!! tip "Same loaders for both"
    If `--loader2` is omitted, the same loaders from `--loader1` are used for both configs. The environment argument determines which env section is resolved.

---

### debug

Show source tracking information for configuration keys.

```bash
confii debug [env] -l type:source [--key key] [--export-report path]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--key` | Specific key to debug (all keys if omitted) |
| `--export-report` | Export full debug report as JSON file |

**Examples:**

```bash
# Debug a specific key
confii debug production -l yaml:base.yaml -l yaml:prod.yaml --key database.host
# Output:
# Key:       database.host
# Value:     prod-db.example.com
# Source:    prod.yaml
# Loader:    YAMLLoader
# Overrides: 1
# History:
#   1. localhost (from base.yaml via YAMLLoader)

# Debug all keys
confii debug production -l yaml:config.yaml

# Export full debug report
confii debug production -l yaml:config.yaml --export-report report.json
```

!!! note "Debug mode is automatic"
    The `debug` command automatically enables `WithDebugMode(true)` for full override history tracking.

---

### explain

Show detailed resolution information for a specific key.

```bash
confii explain [env] -l type:source --key key
```

**Flags:**

| Flag | Description | Required |
|------|-------------|----------|
| `--key` | Key to explain | Yes |

**Examples:**

```bash
confii explain production -l yaml:config.yaml --key database.host
# Output:
# exists:            true
# key:               database.host
# value:             prod-db.example.com
# current_value:     prod-db.example.com
# source:            config.yaml
# loader_type:       YAMLLoader
# environment:       production
# override_count:    1
```

---

### lint

Check configuration for common issues (nil values, empty config).

```bash
confii lint [env] -l type:source [--strict]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--strict` | Exit with code 1 if issues are found |

**Examples:**

```bash
# Lint with warnings
confii lint production -l yaml:config.yaml
# Output: No issues found.

# Strict mode (for CI)
confii lint production -l yaml:config.yaml --strict
# Exits with code 1 if any issues
```

---

### docs

Generate configuration documentation.

```bash
confii docs [env] -l type:source [-f format] [-o output]
```

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `-f, --format` | Output format (`markdown`, `json`) | `markdown` |
| `-o, --output` | Output file path | stdout |

**Examples:**

```bash
# Generate markdown docs
confii docs production -l yaml:config.yaml -f markdown

# Save to file
confii docs production -l yaml:config.yaml -f markdown -o CONFIG.md

# JSON format
confii docs production -l yaml:config.yaml -f json -o config-docs.json
```

---

### migrate

Migrate configuration from other tools or formats.

```bash
confii migrate <source-type> <config-file> [-o output] [--target-format format]
```

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `-o, --output` | Output file path | stdout |
| `--target-format` | Target format (`yaml`, `json`, `toml`) | `yaml` |

**Supported source types:** `dotenv`, `env`, `dynaconf`, `hydra`, `omegaconf`

**Examples:**

```bash
# Migrate .env to YAML
confii migrate dotenv .env -o config.yaml

# Migrate to JSON
confii migrate dotenv .env --target-format json -o config.json

# Migrate from another YAML-based tool
confii migrate hydra config.yaml -o confii-config.yaml
```

---

## Common Workflows

### Inspect a config before deploying

```bash
# Load and review
confii load production -l yaml:config.yaml

# Validate against schema
confii validate production -l yaml:config.yaml --schema schema.json

# Lint for issues
confii lint production -l yaml:config.yaml --strict
```

### Compare environments

```bash
# See what differs between dev and prod
confii diff dev production --loader1 yaml:config.yaml

# Export diff as JSON for review
confii diff dev production --loader1 yaml:config.yaml -f json > env-diff.json
```

### Debug a specific value

```bash
# Where does this value come from?
confii explain production -l yaml:base.yaml -l yaml:prod.yaml --key database.host

# Full debug report
confii debug production -l yaml:base.yaml -l yaml:prod.yaml --export-report debug.json
```

### Generate documentation

```bash
# Auto-generate a config reference
confii docs production -l yaml:config.yaml -f markdown -o docs/CONFIG.md
```

### Migrate from another tool

```bash
# Convert .env to YAML
confii migrate dotenv .env -o config.yaml

# Then validate the result
confii validate -l yaml:config.yaml --schema schema.json
```
