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

## Verify

Preview initialization without writing anything:

```bash
confii init --dry-run --non-interactive
```

Continue with the [Quick Start](quickstart.md) to initialize an empty Go
project, inspect the generated load plan, and run a typed application in
development and production.
