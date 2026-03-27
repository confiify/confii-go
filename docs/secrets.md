# Secret Management

Confii resolves `${secret:key}` placeholders in configuration values at access time through the hook system. Secrets are fetched from pluggable stores -- from simple in-memory dictionaries to cloud providers like AWS Secrets Manager, Azure Key Vault, GCP Secret Manager, and HashiCorp Vault.

---

## How Placeholders Work

When a config value contains a `${secret:...}` placeholder, the secret resolver (registered as a global hook) replaces it with the actual secret value. This happens transparently during `Get` calls -- your application code reads resolved values without knowing they came from a secret store.

```yaml title="config.yaml"
database:
  host: prod-db.example.com
  password: ${secret:db/password}
  url: postgres://admin:${secret:db/password}@prod-db:5432/mydb
```

```go
password, _ := cfg.Get("database.password")
// "s3cret-passw0rd" (resolved from secret store)

url, _ := cfg.Get("database.url")
// "postgres://admin:s3cret-passw0rd@prod-db:5432/mydb" (inline replacement)
```

---

## Placeholder Formats

Three formats with increasing specificity:

### Basic: `${secret:key}`

Fetch the entire secret value by key:

```yaml
api_key: ${secret:services/api-key}
```

### With JSON Path: `${secret:key:json_path}`

When a secret is a JSON object, extract a specific field using dot-notation:

```yaml
# Secret "db/credentials" contains: {"username": "admin", "password": "s3cret"}
db_user: ${secret:db/credentials:username}
db_pass: ${secret:db/credentials:json_path}
```

The JSON path supports nested traversal:

```yaml
# Secret "config/nested" contains: {"level1": {"level2": {"value": "deep"}}}
deep_value: ${secret:config/nested:level1.level2.value}
```

### With Version: `${secret:key:json_path:version}`

Fetch a specific version of the secret:

```yaml
# Fetch version 2 of the secret, extract the "password" field
db_pass: ${secret:db/credentials:password:2}

# Fetch version "AWSPREVIOUS" (AWS-specific stage)
old_key: ${secret:api/key::AWSPREVIOUS}
```

!!! note "Empty JSON path"
    Use an empty JSON path segment to skip it when you only need versioning: `${secret:key::version}`.

---

## Built-in Stores

### DictStore

In-memory store for testing and development. Supports versioning via `SetSecret`.

```go
import "github.com/confiify/confii-go/secret"

store := secret.NewDictStore(map[string]any{
    "db/password":  "s3cret",
    "api/key":      "ak-12345",
    "config/nested": map[string]any{
        "username": "admin",
        "password": "hunter2",
    },
})

// Additional operations
store.SetSecret(ctx, "db/password", "new-password")  // creates a new version
store.DeleteSecret(ctx, "api/key")
keys, _ := store.ListSecrets(ctx, "db/")              // ["db/password"]
store.Clear()                                           // remove all
```

### EnvStore

Retrieves secrets from OS environment variables. Keys are transformed to uppercase with `/`, `.`, and `-` replaced by `_`.

```go
import "github.com/confiify/confii-go/secret"

store := secret.NewEnvStore(
    secret.WithEnvPrefix("SECRET_"),    // prepend prefix
    secret.WithEnvSuffix("_VALUE"),     // append suffix
    secret.WithTransformKey(true),      // default: uppercase + replace separators
)
```

Key transformation example:

```text
Secret key: "db/password"
→ Transform: "DB_PASSWORD"
→ With prefix/suffix: "SECRET_DB_PASSWORD_VALUE"
→ Looks up: os.Getenv("SECRET_DB_PASSWORD_VALUE")
```

```bash
export SECRET_DB_PASSWORD_VALUE=s3cret
```

### MultiStore

Tries multiple stores in priority order. The first store that successfully returns a value wins.

```go
import "github.com/confiify/confii-go/secret"

multi := secret.NewMultiStore(
    []confii.SecretStore{vaultStore, awsStore, envStore},
    secret.WithFailOnMissing(true),   // error if no store has the key
    secret.WithWriteToFirst(true),    // writes go to first store only
)
```

Fallback behavior:

```text
GetSecret("db/password"):
  1. Try vaultStore → not found
  2. Try awsStore   → found! return value
  (envStore is never tried)
```

!!! tip "Order matters"
    Put your most authoritative store first. Cloud stores should come before the env fallback for production, but you might reverse this order for local development.

---

## Cloud Stores

Cloud stores require build tags to compile. This keeps the binary small when you don't need them.

### AWS Secrets Manager

```bash
go build -tags aws
```

```go
import "github.com/confiify/confii-go/secret/cloud"

store, err := cloud.NewAWSSecretsManager(ctx,
    cloud.WithAWSRegion("us-east-1"),
    cloud.WithAWSCredentials("AKIA...", "secret...", ""),  // optional, uses default chain
    cloud.WithAWSEndpoint("http://localhost:4566"),        // LocalStack for testing
)
```

AWS-specific version stages: `AWSCURRENT`, `AWSPENDING`, `AWSPREVIOUS` are recognized as stage names rather than version IDs.

### Azure Key Vault

```bash
go build -tags azure
```

```go
import "github.com/confiify/confii-go/secret/cloud"

// Uses DefaultAzureCredential (managed identity, env vars, CLI, etc.)
store, err := cloud.NewAzureKeyVault(
    "https://my-vault.vault.azure.net",
    nil, // nil = DefaultAzureCredential
)
```

!!! warning "Azure Key Vault name restrictions"
    Secret names must match `^[0-9a-zA-Z-]+$`. Names with `/`, `.`, or `_` will be rejected.

### GCP Secret Manager

```bash
go build -tags gcp
```

```go
import "github.com/confiify/confii-go/secret/cloud"

store, err := cloud.NewGCPSecretManager(ctx,
    "my-gcp-project",
    cloud.WithGCPCredentialsFile("/path/to/service-account.json"), // optional
)
```

When no version is specified, GCP defaults to `"latest"`.

### HashiCorp Vault

```bash
go build -tags vault
```

```go
import "github.com/confiify/confii-go/secret/cloud"

store, err := cloud.NewHashiCorpVault(
    cloud.WithVaultURL("https://vault.example.com:8200"),
    cloud.WithVaultToken("hvs.xxxxx"),
    cloud.WithVaultNamespace("my-team"),
    cloud.WithVaultMountPoint("secret"),   // default: "secret"
    cloud.WithVaultKVVersion(2),           // default: 2
    cloud.WithVaultVerify(true),           // TLS verification, default: true
)
```

Vault also supports the `"path:field"` syntax for extracting specific fields:

```go
// Fetch only the "password" field from secret/data/db/credentials
val, _ := store.GetSecret(ctx, "db/credentials:password")
```

---

## Vault Auth Methods

HashiCorp Vault supports 9 authentication methods. Pass them via `WithVaultAuth`:

=== "Token"

    ```go
    cloud.WithVaultAuth(&cloud.TokenAuth{
        Token: "hvs.xxxxx",
    })
    ```

=== "AppRole"

    ```go
    cloud.WithVaultAuth(&cloud.AppRoleAuth{
        RoleID:     "role-id",
        SecretID:   "secret-id",
        MountPoint: "approle",  // default: "approle"
    })
    ```

=== "LDAP"

    ```go
    cloud.WithVaultAuth(&cloud.LDAPAuth{
        Username:   "admin",
        Password:   "password",
        MountPoint: "ldap",  // default: "ldap"
    })
    // Or with a password provider function:
    cloud.WithVaultAuth(&cloud.LDAPAuth{
        Username: "admin",
        PasswordProvider: func() (string, error) {
            return os.Getenv("VAULT_LDAP_PASSWORD"), nil
        },
    })
    ```

=== "JWT"

    ```go
    cloud.WithVaultAuth(&cloud.JWTAuth{
        Role:       "my-role",
        JWT:        "eyJhbGci...",
        MountPoint: "jwt",  // default: "jwt"
    })
    ```

=== "Kubernetes"

    ```go
    cloud.WithVaultAuth(&cloud.KubernetesAuth{
        Role:       "my-k8s-role",
        JWT:        string(serviceAccountToken),
        MountPoint: "kubernetes",  // default: "kubernetes"
    })
    ```

=== "AWS IAM"

    ```go
    cloud.WithVaultAuth(&cloud.AWSIAMAuth{
        Role:       "my-aws-role",
        MountPoint: "aws",  // default: "aws"
    })
    ```

=== "Azure"

    ```go
    cloud.WithVaultAuth(&cloud.AzureAuth{
        Role:       "my-azure-role",
        Resource:   "https://vault.example.com",  // optional
        MountPoint: "azure",  // default: "azure"
    })
    ```

=== "GCP"

    ```go
    cloud.WithVaultAuth(&cloud.GCPAuth{
        Role:       "my-gcp-role",
        JWT:        "eyJhbGci...",  // optional for GCE metadata
        MountPoint: "gcp",  // default: "gcp"
    })
    ```

=== "OIDC"

    ```go
    cloud.WithVaultAuth(&cloud.OIDCAuth{
        Role:       "my-oidc-role",
        MountPoint: "oidc",  // default: "oidc"
    })
    ```

You can also use the shorthand `WithVaultAppRole` for AppRole auth:

```go
cloud.WithVaultAppRole("role-id", "secret-id")
```

---

## Resolver Options

The `Resolver` bridges a secret store with the hook system:

```go
import "github.com/confiify/confii-go/secret"

resolver := secret.NewResolver(store,
    secret.WithCache(true),                    // enable caching (default: true)
    secret.WithCacheTTL(5 * time.Minute),      // cache expiration (0 = no expiry)
    secret.WithResolverPrefix("prod/"),         // prepend to all keys
    secret.WithResolverFailOnMissing(true),    // error on unresolved secrets (default: true)
)
```

| Option | Default | Description |
|---|---|---|
| `WithCache(bool)` | `true` | Enable/disable internal cache |
| `WithCacheTTL(duration)` | `0` (no expiry) | How long cached values are valid |
| `WithResolverPrefix(string)` | `""` | Prepended to all secret keys before lookup |
| `WithResolverFailOnMissing(bool)` | `true` | Return error for unresolvable secrets |

### Cache Management

```go
// View cache statistics
stats := resolver.CacheStats()
// {"enabled": true, "size": 5, "keys": ["db/password:", ...]}

// Pre-populate cache at startup
resolver.Prefetch(ctx, []string{"db/password", "api/key", "tls/cert"})

// Clear all cached values
resolver.ClearCache()
```

---

## Wiring Resolver with HookProcessor

The resolver's `Hook()` method returns a `hook.Func` that you register as a global hook:

```go
cfg, _ := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
)

// Create store and resolver
store := secret.NewDictStore(map[string]any{
    "db/password": "s3cret",
    "api/key":     "ak-12345",
})
resolver := secret.NewResolver(store,
    secret.WithCache(true),
    secret.WithCacheTTL(5 * time.Minute),
)

// Register as global hook
cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())

// Now all ${secret:...} placeholders are resolved automatically
password, _ := cfg.Get("database.password")
// "s3cret"
```

!!! tip "Hook ordering matters"
    The secret resolver hook should typically be registered **after** the env expander hook. This way, `${VAR}` expansion happens first (resolving env vars), and then `${secret:...}` resolution runs on the result. Since built-in hooks (env expander, type cast) are registered during `New()`, your custom hooks added after creation will naturally run later in the global hook chain.

---

## Multi-Store Fallback Chain

Combine multiple stores for environment-flexible secret resolution:

```go
package main

import (
    "context"
    "time"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
    "github.com/confiify/confii-go/secret"
    "github.com/confiify/confii-go/secret/cloud"
)

func main() {
    ctx := context.Background()

    // Primary: HashiCorp Vault
    vaultStore, _ := cloud.NewHashiCorpVault(
        cloud.WithVaultURL("https://vault.example.com:8200"),
        cloud.WithVaultAuth(&cloud.AppRoleAuth{
            RoleID:   "my-role-id",
            SecretID: "my-secret-id",
        }),
    )

    // Secondary: AWS Secrets Manager
    awsStore, _ := cloud.NewAWSSecretsManager(ctx,
        cloud.WithAWSRegion("us-east-1"),
    )

    // Fallback: Environment variables
    envStore := secret.NewEnvStore(
        secret.WithEnvPrefix("SECRET_"),
    )

    // Multi-store: try Vault, then AWS, then env vars
    multi := secret.NewMultiStore(
        []confii.SecretStore{vaultStore, awsStore, envStore},
        secret.WithFailOnMissing(true),
    )

    // Resolver with caching
    resolver := secret.NewResolver(multi,
        secret.WithCache(true),
        secret.WithCacheTTL(10 * time.Minute),
    )

    // Load config and wire up secret resolution
    cfg, _ := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
    )
    cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())

    // All ${secret:...} placeholders are now resolved through the chain
    dbPass, _ := cfg.Get("database.password")
    apiKey, _ := cfg.Get("api.key")
    _ = dbPass
    _ = apiKey
}
```

---

## Complete Example

```go title="main.go"
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/confiify/confii-go"
    "github.com/confiify/confii-go/loader"
    "github.com/confiify/confii-go/secret"
)

func main() {
    ctx := context.Background()

    // Create a secret store (DictStore for demo; use cloud stores in production)
    store := secret.NewDictStore(map[string]any{
        "db/password": "super-s3cret",
        "api/credentials": map[string]any{
            "key":    "ak-prod-12345",
            "secret": "sk-prod-67890",
        },
        "tls/cert": "-----BEGIN CERTIFICATE-----\n...",
    })

    // Create resolver with caching
    resolver := secret.NewResolver(store,
        secret.WithCache(true),
        secret.WithCacheTTL(5 * time.Minute),
        secret.WithResolverFailOnMissing(true),
    )

    // Pre-fetch critical secrets
    _ = resolver.Prefetch(ctx, []string{"db/password", "api/credentials"})

    // Load config
    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
    )
    if err != nil {
        panic(err)
    }

    // Wire secret resolver into hook system
    cfg.HookProcessor().RegisterGlobalHook(resolver.Hook())

    // Access resolved values
    dbPass, _ := cfg.Get("database.password")
    fmt.Println("DB Password:", dbPass)
    // "super-s3cret"

    apiKey, _ := cfg.Get("api.key")
    fmt.Println("API Key:", apiKey)
    // "ak-prod-12345" (extracted via JSON path)

    dbURL, _ := cfg.Get("database.url")
    fmt.Println("DB URL:", dbURL)
    // "postgres://admin:super-s3cret@prod-db:5432/mydb"

    // Cache stats
    stats := resolver.CacheStats()
    fmt.Printf("Cache: %d entries\n", stats["size"])
}
```

```yaml title="config.yaml"
default:
  database:
    host: localhost
    port: 5432
    password: ${secret:db/password}
    url: postgres://admin:${secret:db/password}@localhost:5432/mydb
  api:
    key: ${secret:api/credentials:key}
    secret: ${secret:api/credentials:secret}

production:
  database:
    host: prod-db.example.com
    url: postgres://admin:${secret:db/password}@prod-db:5432/mydb
```
