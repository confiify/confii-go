# Installation

## Core Library

`go get` must run inside a Go module because it records Confii in that
project's `go.mod` and `go.sum`. From the root of an existing module:

```bash
go get github.com/confiify/confii-go@latest
```

Requires **Go 1.25+**.

From an empty directory, create the module first:

```bash
mkdir my-service
cd my-service
go mod init example.com/my-service
go get github.com/confiify/confii-go@latest
```

Use the repository's real module path instead of `example.com/my-service`.

This adds Confii to the current project's `go.mod`. For reproducible builds,
commit `go.mod` and `go.sum`; production projects may replace `@latest` with the
explicit `@vX.Y.Z` release selected by their dependency policy.

The core module pulls in only the dependencies needed for file, environment,
and HTTP loaders, plus validation and secret-resolution machinery. The Git
loader lives in the separate `loader/cloud` module but does not require a
build tag. The core module does **not** pull in any cloud provider SDKs.

## Cloud Providers (separate modules + build tags)

Cloud loaders and secret stores are separate Go modules. Installing the core
module therefore does not pull AWS, Azure, GCP, IBM, or Vault SDKs into an
application that does not use them.

Install one or both cloud modules, depending on which APIs your application
uses:

```bash
go get github.com/confiify/confii-go/loader/cloud@latest
go get github.com/confiify/confii-go/secret/cloud@latest
```

The cloud modules declare and checksum their supported SDK versions. You do
not need to add provider SDKs by hand. Build tags select the provider source
files that are compiled:

| Provider | Build tag | Available integrations |
| --- | --- | --- |
| AWS | `aws` | S3, SSM Parameter Store, Secrets Manager |
| Azure | `azure` | Blob Storage, Key Vault |
| GCP | `gcp` | Cloud Storage, Secret Manager |
| HashiCorp Vault | `vault` | Vault secret store and auth backends |
| IBM Cloud | `ibm` | Cloud Object Storage |
| GitHub/GitLab raw content | none | Git loader (always available after adding `loader/cloud`) |

For example:

```bash
go build -tags aws ./...
go test -tags vault ./...
```

### Combining providers

```bash
go build -tags "aws,vault" ./...
```

The same provider imports enable declarative `.confii.yaml` source types and
the value-safe `connections test` preflight when embedded through
`confii/cmd.NewRootCommand`; see the [CLI connection test](cli.md#connections-test).

Repository contributors can verify every module and all tagged packages with:

```bash
make mod-verify
make test-cloud
# or one provider / one gate:
sh scripts/test-cloud-consumer.sh aws vet
```

The cloud helper creates a temporary consumer module, replaces the three
publishable modules with the local checkout, and removes the fixture after the
command completes.

!!! tip "Only include what you need"
    Add only the cloud module your application imports. Build tags determine
    which provider implementation is linked into the final binary, while the
    root module remains independently tidy and cloud-SDK-free.

## CLI Tool

The CLI is a separate executable. Unlike `go get`, this version-qualified
`go install` command may run from any directory and does not modify an
application's `go.mod`:

```bash
go install github.com/confiify/confii-go/confii@latest
```

Verify the installation:

```bash
confii --version
```

A version-qualified install reports that module version. `dev` is reserved for
an unversioned local build from a working tree.

`go install` writes the executable to `GOBIN`, or to `$(go env GOPATH)/bin`
when `GOBIN` is unset. Add that directory to `PATH` if your shell cannot find
`confii`.

Remove the CLI from that same Go installation directory with:

```bash
make uninstall
```

The goal removes only the resolved `confii` executable. It refuses to remove a
directory and succeeds without changing anything when the executable is
already absent. A normal released installation can be restored at any time
with the version-qualified `go install` command above.

### Install a development checkout

Contributors and prerelease testers should install from the checked-out source
through the repository Makefile rather than using `@latest`:

```bash
git switch feat/my-change
make install-dev
confii --version
```

The installed version is commit-aware, for example
`dev-18eba047d69b` or `dev-18eba047d69b-dirty`. The `-dirty` suffix means tracked
working-tree changes were present, so test reports can identify builds that do
not correspond exactly to a commit. `make install` remains an alias for
`make install-dev`.

By default, the goal installs to `GOBIN`, or to `$(go env GOPATH)/bin` when
`GOBIN` is unset. To test without replacing an existing `confii` executable on
your `PATH`, select an isolated directory:

```bash
make install-dev INSTALL_DIR="$PWD/bin/dev-install"
./bin/dev-install/confii --version
```

Remove an isolated installation by supplying the same directory:

```bash
make uninstall INSTALL_DIR="$PWD/bin/dev-install"
```

Re-run `make install-dev` after changing branches or commits. The Go tool does
not automatically refresh an already installed development executable.

### Use an unreleased library in another project

Installing the CLI does not change the Confii library selected by another Go
module. Choose one of these workflows depending on what you need to test.

For a pushed feature branch, resolve its commit and ask Go to record that exact
revision as a pseudo-version in the consumer project's `go.mod`:

```bash
CONFII_DEV_REF="$(git -C /path/to/confii-go rev-parse HEAD)"
cd /path/to/consumer
go get "github.com/confiify/confii-go@${CONFII_DEV_REF}"
go mod tidy
go test ./...
go list -m github.com/confiify/confii-go
```

The commit must already exist on the remote. `go.mod` records an immutable
pseudo-version containing its timestamp and hash. This is the preferred way to
share a committed prerelease build with another developer or CI job. A Git
branch containing `/`, such as `feat/environment-management-cli`, is not a
valid Go module version query, so use its commit rather than passing that branch
name after `@`. Do not release the consumer while it still depends on the
pseudo-version.

Applications importing the optional cloud modules must select the same branch
for all three independently versioned modules:

```bash
go get "github.com/confiify/confii-go@${CONFII_DEV_REF}"
go get "github.com/confiify/confii-go/loader/cloud@${CONFII_DEV_REF}"
go get "github.com/confiify/confii-go/secret/cloud@${CONFII_DEV_REF}"
go mod tidy
```

For local changes that have not been pushed, link the consumer directly to the
working tree. Run the goal from the Confii checkout:

```bash
make consumer-link-dev CONSUMER_DIR=/absolute/path/to/consumer
```

For a consumer that imports cloud loaders or secret stores, link all three
modules together:

```bash
make consumer-link-dev-cloud CONSUMER_DIR=/absolute/path/to/consumer
```

The goals add standard Go `replace` directives but deliberately do not run
`go mod tidy` or tests on someone else's project. Review the resulting
`go.mod`, then run the consumer's own workflow:

```bash
cd /absolute/path/to/consumer
go mod tidy
go test ./...
go list -m -json github.com/confiify/confii-go
```

The `Replace.Dir` field in the final command should point at the local Confii
checkout. Local replacements consume the working tree, including uncommitted
changes; the commit-aware CLI version cannot describe those library changes.

Remove every Confii development replacement before committing or releasing
the consumer:

```bash
cd /path/to/confii-go
make consumer-unlink-dev CONSUMER_DIR=/absolute/path/to/consumer
cd /absolute/path/to/consumer
go mod tidy
go test ./...
```

Check `git diff -- go.mod go.sum` after either workflow. Local filesystem paths
must never be committed to a portable consumer module.

## Verify

Preview initialization without writing anything:

```bash
confii init --dry-run --non-interactive
```

Continue with the [Quick Start](quickstart.md) to initialize an empty Go
project, inspect the generated load plan, and run a typed application in
development and production.
