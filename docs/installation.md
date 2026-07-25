# Installation

## Core Library

```bash
go get github.com/confiify/confii-go@v1.2.1
```

Requires **Go 1.25+**.

The core module pulls in only the dependencies needed for file, environment,
HTTP, and Git loaders, plus validation and secret-resolution machinery. It
does **not** pull in any cloud provider SDKs.

## Cloud Providers (separate modules + build tags)

Cloud loaders and secret stores are separate Go modules. Installing the core
module therefore does not pull AWS, Azure, GCP, IBM, or Vault SDKs into an
application that does not use them.

Install one or both cloud modules, depending on which APIs your application
uses:

```bash
go get github.com/confiify/confii-go/loader/cloud@v1.2.1
go get github.com/confiify/confii-go/secret/cloud@v1.2.1
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

```bash
go install github.com/confiify/confii-go/confii@v1.2.1
```

Verify the installation:

```bash
confii --help
```

## Verify

```go
package main

import (
    "context"
    "fmt"
    "log"

    confii "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
)

func main() {
    cfg, err := confii.New[any](context.Background(),
        confii.WithLoaders(loader.NewYAML("config.yaml")),
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Keys:", cfg.Keys())
}
```
