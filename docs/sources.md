# Configuration Sources

Confii loads configuration from files, environment variables, HTTP endpoints, and
cloud storage -- all through a unified `Loader` interface. Sources are loaded in
order: **later loaders override earlier ones** when deep merge is enabled.

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("config/base.yaml"),       // loaded first (lowest priority)
        loader.NewJSON("config/overrides.json"),   // overrides base
        loader.NewEnvironment("APP"),              // overrides everything
    ),
)
```

---

## File Loaders

Confii supports five file formats out of the box with no build tags required.

### YAML

```go
import "github.com/confiify/confii-go/loader"

l := loader.NewYAML("config.yaml")
```

```yaml title="config.yaml"
database:
  host: localhost
  port: 5432
  credentials:
    username: admin
    password: ${secret:db/password}
```

### JSON

```go
l := loader.NewJSON("config.json")
```

```json title="config.json"
{
  "database": {
    "host": "localhost",
    "port": 5432
  }
}
```

### TOML

```go
l := loader.NewTOML("config.toml")
```

```toml title="config.toml"
[database]
host = "localhost"
port = 5432
```

### INI

```go
l := loader.NewINI("config.ini")
```

```ini title="config.ini"
[database]
host = localhost
port = 5432
```

### .env (Dotenv)

```go
l := loader.NewEnvFile(".env")
```

```bash title=".env"
DATABASE_HOST=localhost
DATABASE_PORT=5432
DEBUG=true
```

!!! tip "Combining file formats"
    You can mix formats freely. A common pattern is to use YAML for the main
    config, JSON for machine-generated overrides, and `.env` for local secrets:

    ```go
    confii.WithLoaders(
        loader.NewYAML("config.yaml"),
        loader.NewJSON("generated.json"),
        loader.NewEnvFile(".env.local"),
    )
    ```

---

## Environment Variables

The `EnvironmentLoader` reads OS environment variables matching a prefix and maps
them into nested configuration keys.

```go
l := loader.NewEnvironment("APP")
```

### How Variables Map to Keys

Given prefix `APP`, the loader:

1. Filters variables starting with `APP_`
2. Strips the `APP_` prefix
3. Splits on the separator (default `__`) to create nested keys
4. Lowercases all key parts

| Environment Variable | Config Key | Value |
| --- | --- | --- |
| `APP_DEBUG` | `debug` | `true` |
| `APP_SERVER__HOST` | `server.host` | `"0.0.0.0"` |
| `APP_SERVER__PORT` | `server.port` | `8080` |
| `APP_DATABASE__MAX_CONNECTIONS` | `database.max_connections` | `100` |

!!! note "Scalar type parsing"
    Values are automatically parsed: `"true"` becomes `bool`, `"8080"` becomes
    `int`, `"3.14"` becomes `float64`. Unparseable values stay as strings.

### Custom Separator

The default nesting separator is `__` (double underscore). Override it with
`WithSeparator`:

```go
l := loader.NewEnvironment("APP", loader.WithSeparator("_"))
```

With `WithSeparator("_")`, `APP_DATABASE_HOST` maps to `database.host`.

!!! warning "Single underscore separator"
    Using `_` as the separator means you cannot have keys with underscores in
    their names. Prefer the default `__` unless you have a specific reason to
    change it.

### Using WithEnvPrefix

As a shorthand, you can use `confii.WithEnvPrefix` instead of explicitly adding
an `EnvironmentLoader`:

```go
// These are equivalent:
confii.WithLoaders(loader.NewEnvironment("APP"))
// vs
confii.WithEnvPrefix("APP")
```

### Full Example

=== "Shell"

    ```bash
    export APP_SERVER__HOST=0.0.0.0
    export APP_SERVER__PORT=9090
    export APP_DATABASE__HOST=prod-db.example.com
    export APP_DATABASE__SSL=true
    ```

=== "Go"

    ```go
    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(
            loader.NewYAML("config.yaml"),
            loader.NewEnvironment("APP"),      // overrides YAML values
        ),
    )

    host := cfg.GetStringOr("server.host", "localhost")
    // "0.0.0.0" (from environment)
    ```

---

## HTTP Loader

Load configuration from any HTTP or HTTPS endpoint. The response body is
auto-detected as JSON or YAML based on the `Content-Type` header.

```go
import "github.com/confiify/confii-go/loader"

l := loader.NewHTTP("https://config.example.com/api/v1/config")
```

### Options

| Option | Description | Default |
| --- | --- | --- |
| `loader.WithTimeout(d)` | HTTP request timeout | `30s` |
| `loader.WithHeaders(map)` | Custom request headers | none |
| `loader.WithBasicAuth(user, pass)` | HTTP Basic Authentication | none |

### Examples

=== "Basic"

    ```go
    l := loader.NewHTTP("https://config.example.com/app.json")
    ```

=== "With Timeout"

    ```go
    l := loader.NewHTTP("https://config.example.com/app.json",
        loader.WithTimeout(10 * time.Second),
    )
    ```

=== "With Auth"

    ```go
    l := loader.NewHTTP("https://config.example.com/app.json",
        loader.WithBasicAuth("admin", "secret"),
        loader.WithHeaders(map[string]string{
            "Accept": "application/json",
        }),
    )
    ```

=== "With Bearer Token"

    ```go
    l := loader.NewHTTP("https://config.example.com/app.json",
        loader.WithHeaders(map[string]string{
            "Authorization": "Bearer " + os.Getenv("CONFIG_TOKEN"),
        }),
    )
    ```

!!! note "Content-Type detection"
    The HTTP loader inspects the `Content-Type` response header to determine the
    format. If the header is missing or ambiguous, it falls back to parsing the
    URL extension (`.json`, `.yaml`, `.yml`, `.toml`). JSON and YAML are
    attempted in order if no format can be determined.

---

## Cloud Loaders

Cloud loaders are **opt-in via build tags** to avoid pulling in heavy SDKs when
you don't need them.

### Build Tags Overview

| Build Tag | Enabled Loaders | SDK |
| --- | --- | --- |
| `aws` | S3, SSM Parameter Store | `aws-sdk-go-v2` |
| `azure` | Azure Blob Storage | `azure-sdk-for-go` |
| `gcp` | Google Cloud Storage | `cloud.google.com/go` |
| `ibm` | IBM Cloud Object Storage | IBM COS SDK |

```bash
# Enable specific providers
go build -tags aws
go build -tags "aws,gcp"

# Enable all cloud providers
go build -tags "aws,azure,gcp,ibm"
```

!!! warning "Build tags are required"
    Without the appropriate build tag, the cloud loader constructors will not be
    available at compile time. You will get a build error if you try to import
    a cloud loader without its tag.

### Git Loader (No Build Tag Required)

The Git loader fetches configuration from a file in a GitHub or GitLab
repository via raw content URLs. It does **not** require any build tag.

```go
import "github.com/confiify/confii-go/loader/cloud"

l := cloud.NewGit(
    "https://github.com/myorg/config-repo",
    "services/my-app/config.yaml",
)
```

#### Git Options

| Option | Description | Default |
| --- | --- | --- |
| `cloud.WithGitBranch(branch)` | Branch to read from | `"main"` |
| `cloud.WithGitToken(token)` | Access token for private repos | `$GIT_TOKEN` env var |

```go
l := cloud.NewGit(
    "https://github.com/myorg/config-repo",
    "config.yaml",
    cloud.WithGitBranch("release/v2"),
    cloud.WithGitToken(os.Getenv("GITHUB_TOKEN")),
)
```

---

### AWS S3

Loads a config file from an S3 bucket. Requires build tag `aws`.

```go
//go:build aws

import "github.com/confiify/confii-go/loader/cloud"

l, err := cloud.NewS3("s3://my-bucket/config/app.yaml")
```

#### S3 Options

| Option | Description | Default |
| --- | --- | --- |
| `cloud.WithS3Region(region)` | AWS region | auto-detected |
| `cloud.WithS3Credentials(access, secret)` | Explicit credentials | default credential chain |

```go
l, err := cloud.NewS3("s3://my-bucket/config/app.yaml",
    cloud.WithS3Region("us-west-2"),
    cloud.WithS3Credentials(
        os.Getenv("AWS_ACCESS_KEY_ID"),
        os.Getenv("AWS_SECRET_ACCESS_KEY"),
    ),
)
```

!!! tip "S3 URL format"
    The S3 URL follows the standard `s3://bucket-name/key/path` format.
    The file format is auto-detected from the key's extension.

---

### AWS SSM Parameter Store

Loads configuration from AWS Systems Manager Parameter Store by path prefix.
All parameters under the prefix are read and organized into a nested map.
Requires build tag `aws`.

```go
//go:build aws

import "github.com/confiify/confii-go/loader/cloud"

l := cloud.NewSSM("/myapp/production/")
```

#### SSM Options

| Option | Description | Default |
| --- | --- | --- |
| `cloud.WithSSMDecrypt(bool)` | Decrypt SecureString parameters | `true` |
| `cloud.WithSSMRegion(region)` | AWS region | auto-detected |
| `cloud.WithSSMCredentials(access, secret)` | Explicit credentials | default credential chain |

```go
l := cloud.NewSSM("/myapp/production/",
    cloud.WithSSMRegion("eu-west-1"),
    cloud.WithSSMDecrypt(true),
)
```

!!! note "SSM key mapping"
    A parameter at `/myapp/production/database/host` with prefix
    `/myapp/production/` becomes the key `database.host` in your config.

---

### Azure Blob Storage

Loads a config file from Azure Blob Storage. Requires build tag `azure`.

```go
//go:build azure

import "github.com/confiify/confii-go/loader/cloud"

l := cloud.NewAzureBlob(
    "https://myaccount.blob.core.windows.net/configs",
    "app/config.yaml",
)
```

#### Azure Blob Options

| Option | Description |
| --- | --- |
| `cloud.WithAzureAccountKey(name, key)` | Authenticate with account name and key |
| `cloud.WithAzureSASToken(name, token)` | Authenticate with a SAS token |
| `cloud.WithAzureConnectionString(conn)` | Authenticate with a full connection string |

```go
l := cloud.NewAzureBlob(
    "https://myaccount.blob.core.windows.net/configs",
    "app/config.yaml",
    cloud.WithAzureAccountKey("myaccount", os.Getenv("AZURE_STORAGE_KEY")),
)
```

!!! tip "Azure authentication"
    If no explicit credentials are provided, the loader falls back to
    `azidentity.NewDefaultAzureCredential()`, which supports managed identity,
    Azure CLI, and other standard methods.

---

### Google Cloud Storage

Loads a config file from a GCS bucket. Requires build tag `gcp`.

```go
//go:build gcp

import "github.com/confiify/confii-go/loader/cloud"

l := cloud.NewGCS("my-bucket", "config/app.yaml")
```

#### GCS Options

| Option | Description | Default |
| --- | --- | --- |
| `cloud.WithGCSProject(id)` | GCP project ID | auto-detected |
| `cloud.WithGCSCredentials(path)` | Path to service account key file | ADC |

```go
l := cloud.NewGCS("my-bucket", "config/app.yaml",
    cloud.WithGCSProject("my-project-123"),
    cloud.WithGCSCredentials("/etc/secrets/sa-key.json"),
)
```

---

### IBM Cloud Object Storage

Loads a config file from IBM COS. Requires build tag `ibm`.

```go
//go:build ibm

import "github.com/confiify/confii-go/loader/cloud"

l := cloud.NewIBMCOS(/* ... */)
```

---

## Multi-Source Loading Order

When multiple loaders are configured, they are processed **in order**. Each
subsequent loader's data is merged on top of the previous result.

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("config/base.yaml"),       // 1. Base config
        loader.NewYAML("config/prod.yaml"),        // 2. Env-specific overrides
        loader.NewEnvFile(".env"),                  // 3. Local dotenv
        loader.NewEnvironment("APP"),              // 4. Environment variables (highest)
    ),
    confii.WithEnv("production"),
)
```

The effective merge order is:

```
base.yaml  <--merged--  prod.yaml  <--merged--  .env  <--merged--  APP_* env vars
```

!!! note "Deep merge is the default"
    With `WithDeepMerge(true)` (the default), nested maps are merged recursively.
    A later source only needs to specify the keys it wants to override -- all
    other keys from earlier sources are preserved.

### Override Behavior

=== "Deep Merge (default)"

    ```yaml title="base.yaml"
    database:
      host: localhost
      port: 5432
      pool_size: 10
    ```

    ```yaml title="prod.yaml"
    database:
      host: prod-db.example.com
    ```

    **Result:**
    ```yaml
    database:
      host: prod-db.example.com  # from prod.yaml
      port: 5432                  # preserved from base.yaml
      pool_size: 10               # preserved from base.yaml
    ```

=== "Shallow Merge"

    ```yaml title="base.yaml"
    database:
      host: localhost
      port: 5432
      pool_size: 10
    ```

    ```yaml title="prod.yaml"
    database:
      host: prod-db.example.com
    ```

    **Result:**
    ```yaml
    database:
      host: prod-db.example.com  # entire "database" key replaced
      # port and pool_size are LOST
    ```

!!! warning "Shallow merge replaces entire sections"
    With `WithDeepMerge(false)`, a later source that defines `database` will
    replace the **entire** `database` map from earlier sources. Only use shallow
    merge if you understand this behavior and want full section replacement.

### Recommended Loading Order

A common pattern for production applications:

```go
confii.WithLoaders(
    loader.NewYAML("config/defaults.yaml"),    // 1. Shared defaults
    loader.NewYAML("config/" + env + ".yaml"), // 2. Environment-specific
    loader.NewEnvFile(".env"),                  // 3. Local overrides (gitignored)
    loader.NewEnvironment("APP"),              // 4. Runtime overrides (12-factor)
)
```

| Layer | Purpose | Committed to Git? |
| --- | --- | --- |
| `defaults.yaml` | Sane defaults for all environments | Yes |
| `production.yaml` | Production-specific values | Yes |
| `.env` | Developer-local overrides | No (gitignored) |
| `APP_*` env vars | CI/CD and runtime overrides | N/A |

---

## Runtime Source Extension

You can add new sources after initialization without reloading everything:

```go
// Add a new source at runtime
cfg.Extend(ctx, loader.NewJSON("hotfix-config.json"))
```

The extended source is merged on top of the existing configuration using the
same merge strategy.

---

## Custom Loaders

Implement the `Loader` interface to create your own source:

```go
type Loader interface {
    Load(ctx context.Context) (map[string]any, error)
    Source() string
}
```

- `Load` returns the configuration as a `map[string]any`, or `(nil, nil)` if the
  source does not exist (graceful absence).
- `Source` returns a human-readable identifier (e.g., file path, URL).

### Example: Redis Loader

```go
type RedisLoader struct {
    client *redis.Client
    key    string
}

func (l *RedisLoader) Load(ctx context.Context) (map[string]any, error) {
    data, err := l.client.Get(ctx, l.key).Bytes()
    if err == redis.Nil {
        return nil, nil // graceful absence
    }
    if err != nil {
        return nil, err
    }

    var result map[string]any
    if err := json.Unmarshal(data, &result); err != nil {
        return nil, err
    }
    return result, nil
}

func (l *RedisLoader) Source() string {
    return "redis:" + l.key
}
```

```go
cfg, err := confii.New[any](ctx,
    confii.WithLoaders(
        loader.NewYAML("config.yaml"),
        &RedisLoader{client: rdb, key: "app:config"},
    ),
)
```

!!! tip "Graceful absence"
    Return `(nil, nil)` from `Load` when the source simply doesn't exist (e.g.,
    an optional file or a missing Redis key). Return `(nil, error)` for actual
    failures (network errors, parse errors). The error policy (`WithOnError`)
    only applies to actual errors, not graceful absence.
