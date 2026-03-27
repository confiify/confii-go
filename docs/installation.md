# Installation

## Core Library

```bash
go get github.com/confiify/confii-go
```

Requires **Go 1.25+**.

## Cloud Providers (build tags)

Cloud loaders and secret stores are opt-in via build tags to avoid pulling in large SDK dependencies:

```bash
go build -tags aws          # S3, SSM, Secrets Manager
go build -tags azure        # Blob Storage, Key Vault
go build -tags gcp          # Cloud Storage, Secret Manager
go build -tags vault        # HashiCorp Vault
go build -tags ibm          # IBM Cloud Object Storage
go build -tags "aws,azure,gcp,vault,ibm"  # all providers
```

!!! tip "Only include what you need"
    Each build tag adds the corresponding cloud SDK as a dependency. Only enable the providers your application uses to keep binary sizes small.

## CLI Tool

```bash
go install github.com/confiify/confii-go/confii@latest
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
