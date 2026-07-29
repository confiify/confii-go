# Secret Management

Confii resolves `${secret:key}` placeholders while initializing the effective
configuration. It first loads and merges all sources, selects the active
environment, discovers the remaining secret references, deduplicates reads of
the same remote document, and resolves them before `confii.New` returns.
Ordinary getters therefore read a ready in-memory snapshot and do not trigger
provider traffic. Stores are pluggable -- from in-memory dictionaries to AWS
Secrets Manager, Azure Key Vault, GCP Secret Manager, Vault, and OpenBao.

---

## How Placeholders Work

When a config value contains a `${secret:...}` placeholder, the configured
resolver replaces it during startup. Only references in the final selected
environment are contacted; references exclusive to inactive environments are
not. Missing or inaccessible required secrets make initialization fail without
publishing a partially resolved `Config`.

```yaml title="config.yaml"
database:
  host: prod-db.example.com
  password: ${secret:db/password}
  url: postgres://admin:${secret:db/password}@prod-db:5432/mydb
```

```go
password, _ := cfg.Get("database.password")
// already resolved in memory; no provider request occurs here

url, _ := cfg.Get("database.url")
// inline references were also resolved before New returned
```

### Resolution lifecycle

```text
load sources → merge → select environment → discover references
             → deduplicate provider reads → resolve → validate → publish
```

If two keys select different JSON fields from the same secret document, Confii
fetches that document once during the materialization pass and extracts both
fields locally. A normal `Get`, `Typed`, `ToDict`, or `Export` call does not
refresh secrets. Rotation is explicit and transactional:

```go
if err := cfg.RefreshSecrets(ctx); err != nil {
    // The previous ready configuration remains active.
}
```

`Reload` performs the same eager materialization after rebuilding the source
layers. A failed provider read or validation leaves the prior configuration
active. Imperative hooks registered *after* `New` remain access-time hooks for
backward compatibility; applications that want startup resolution must use
declarative `.confii.yaml` providers or `confii.WithSecretHook`.

---

## Placeholder Formats

The default-provider forms have increasing specificity:

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

### With an explicit provider: `${secret@provider:key}`

Declarative named-provider configurations can route each reference to a
specific backend. The provider qualifier works with JSON paths and versions:

```yaml
shared_key: ${secret@vault:platform/signing:key}
payment_key: ${secret@aws-production:payments/api-key::AWSCURRENT}
analytics_token: ${secret@gcp:analytics-token}
```

An unqualified `${secret:key}` uses the default provider selected for the
active environment. `secret@provider` is intentionally distinct from the
colon-delimited key/path/version grammar, so existing references remain
unambiguous and backward compatible.

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

Cloud stores live in the separate `secret/cloud` module and require provider
build tags to compile. This keeps the binary small when you don't need them.

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

### HashiCorp Vault and OpenBao

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

OpenBao uses the same build tag, options, authentication implementations, and
KV v1/v2 behavior. Use the explicit constructor when the server is OpenBao:

```go
store, err := cloud.NewOpenBao(
    cloud.WithVaultURL("https://openbao.example.com:8200"),
    cloud.WithVaultAuth(&cloud.AppRoleAuth{
        RoleID: roleID,
        SecretID: secretID,
    }),
)
```

Confii's CI starts a real, digest-pinned OpenBao 2.6.1 server and verifies KV
write, read, field extraction, list, delete, token authentication, and AppRole
authentication. The shared implementation deliberately retains the existing
`VaultOption` and `HashiCorpVault` names for API compatibility.

Vault also supports the `"path:field"` syntax for extracting specific fields:

```go
// Fetch only the "password" field from secret/data/db/credentials
val, _ := store.GetSecret(ctx, "db/credentials:password")
```

---

## Vault Auth Methods

The Vault-compatible integration implements nine authentication flows. Pass
them via `WithVaultAuth`; availability and server-side configuration of a
method still depend on the selected HashiCorp Vault or OpenBao deployment.
CI live-tests Token and AppRole against OpenBao. The remaining flows have
protocol-level tests and require a real provider-side identity setup before
they can be certified in your deployment:

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
        Role:                  "my-aws-role",
        MountPoint:            "aws",  // default: "aws"
        IAMHTTPRequestMethod:  signed.Method,
        IAMHTTPRequestURL:     signed.URLBase64,
        IAMHTTPRequestBody:    signed.BodyBase64,
        IAMHTTPRequestHeaders: signed.HeadersBase64,
    })
    ```

    The application signs an STS `GetCallerIdentity` request with the AWS SDK and supplies Vault's four base64-encoded IAM request fields.

=== "Azure"

    ```go
    cloud.WithVaultAuth(&cloud.AzureAuth{
        Role:       "my-azure-role",
        JWT:        azureIdentityToken,
        Resource:   "https://vault.example.com",  // optional
        MountPoint: "azure",  // default: "azure"
    })
    ```

=== "GCP"

    ```go
    cloud.WithVaultAuth(&cloud.GCPAuth{
        Role:       "my-gcp-role",
        JWT:        "eyJhbGci...",  // signed IAM/GCE identity JWT
        MountPoint: "gcp",  // default: "gcp"
    })
    ```

=== "OIDC"

    ```go
    cloud.WithVaultAuth(&cloud.OIDCAuth{
        Role:         "my-oidc-role",
        MountPoint:   "oidc",  // default: "oidc"
        RedirectURI:  "http://localhost:8250/oidc/callback",
    })
    ```

    OIDC starts a loopback callback server, opens the provider login in the default browser, validates the returned state and nonce, and exchanges the authorization code with Vault. The redirect URI must be allowed by both the Vault role and the OIDC provider. Embedded/headless applications can set `CallbackProvider` to collect and return the full callback URL themselves; `CallbackTimeout` and `OpenBrowser` customize the interactive flow.

You can also use the shorthand `WithVaultAppRole` for AppRole auth:

```go
cloud.WithVaultAppRole("role-id", "secret-id")
```

---

## Declarative Self-Config Providers

Cloud stores can be wired through `.confii.yaml` when the application blank-imports `github.com/confiify/confii-go/secret/cloud` and builds with the matching tag. Each tagged package registers its provider during `init`.

```yaml
secrets:
  provider: vault
  address: https://vault.internal:8200
  mount_point: secret
  kv_version: 2
  verify: true
  auth:
    method: kubernetes
    role: confii-production
    token_path: /var/run/secrets/kubernetes.io/serviceaccount/token
```

The single-provider form remains supported. For mixed backends, configure
named providers and choose environment-specific defaults:

```yaml
secrets:
  default_provider: vault
  environment_defaults:
    production: aws-production
    analytics: gcp-analytics

  providers:
    vault:
      type: vault
      mount_point: secret
      kv_version: 2
      auth:
        method: token

    aws-production:
      type: aws
      region: us-east-1

    gcp-analytics:
      type: gcp
      project_id: analytics-production

    shared:
      type: vault
      mount_point: shared
      kv_version: 2
      auth:
        method: token
```

With `production` selected, `${secret:database/password}` uses
`aws-production`. `${secret@shared:services/signing:key}` uses `shared` in
every environment. Provider aliases are application-defined; `type` selects
the registered implementation. Factories initialize lazily on the first
reference, so selecting production does not require credentials for an unused
development provider.

The effective order is:

1. An explicit `secret@provider` qualifier.
2. `environment_defaults[active_environment]`.
3. `default_provider`.

An unqualified reference with no effective default fails closed. An unknown
provider alias, unsupported build-tagged provider type, unavailable backend,
missing field, or unsupported versioned read also fails closed.

Provider-specific fields:

| Provider | Required/configuration fields |
| --- | --- |
| `aws` | `region`; optional `access_key`, `secret_key`, `session_token`, `endpoint` (otherwise the AWS default credential chain is used) |
| `azure` | `vault_url` (aliases: `address`, `url`); Azure Default Credential is used |
| `gcp` | `project_id`; optional `credentials_file` (otherwise Application Default Credentials are used) |
| `vault` | `address` or `VAULT_ADDR`; optional `namespace`, `mount_point`, `kv_version`, `verify`, and `auth` |

Vault self-config can declare all nine implemented auth flows: `token`,
`approle`, `ldap`, `jwt`, `kubernetes`, `aws_iam`, `azure`, `gcp`, and
interactive `oidc`. This is configuration support, not a claim that every
method is turnkey or live-certified: Token and AppRole are the CI-tested
OpenBao paths; the others require provider-side identity configuration. `auth`
may be a method string with fields alongside it or a nested map with `method`.
A root `token` or `VAULT_TOKEN` is used for token auth. The same build can
register multiple providers by enabling multiple tags, for example
`-tags="aws,vault"`.

After configuration contains at least one `${secret:...}` reference, run the
value-safe preflight before deployment:

```bash
confii connections test production --timeout 20s
```

The standard installed CLI intentionally has no cloud SDKs. Use an
application operational binary that imports the selected provider modules as
described in the [CLI connection test](cli.md#connections-test). The command
authenticates and performs real reads through the normal resolution hook, then
discards every value.

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

## Imperative resolver wiring

When declarative self-configuration is not appropriate, pass the resolver's
context-aware hook to `New`. Constructor-time wiring participates in eager,
fail-fast materialization:

```go
// Create store and resolver
store := secret.NewDictStore(map[string]any{
    "db/password": "s3cret",
    "api/key":     "ak-12345",
})
resolver := secret.NewResolver(store,
    secret.WithCache(true),
    secret.WithCacheTTL(5 * time.Minute),
)

cfg, err := confii.New[any](ctx,
    confii.WithLoaders(loader.NewYAML("config.yaml")),
    confii.WithSecretResolver(resolver),
)
if err != nil {
    return err
}

// Already resolved; this is an in-memory read.
password, _ := cfg.Get("database.password")
```

!!! tip "Initialization ordering"
    Eager materialization follows the built-in order: environment expansion,
    type casting, then constructor-time secret resolution. Arbitrary hooks
    registered after `New` remain access-time transformations and are not part
    of the startup transaction.

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

    // Load, consolidate, and resolve before returning.
    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
        confii.WithSecretResolver(resolver),
    )
    if err != nil {
        panic(err)
    }

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
            "key":    "ak-prod-12345", // gitleaks:allow -- illustrative value
            "secret": "sk-prod-67890", // gitleaks:allow -- illustrative value
        },
        "tls/cert": "-----BEGIN CERTIFICATE-----\n...",
    })

    // Create resolver with caching
    resolver := secret.NewResolver(store,
        secret.WithCache(true),
        secret.WithCacheTTL(5 * time.Minute),
        secret.WithResolverFailOnMissing(true),
    )

    // Load, resolve all effective references, and validate before returning.
    cfg, err := confii.New[any](ctx,
        confii.WithLoaders(loader.NewYAML("config.yaml")),
        confii.WithEnv("production"),
        confii.WithSecretResolver(resolver),
    )
    if err != nil {
        panic(err)
    }

    // Access already-resolved values without provider traffic.
    dbPass, _ := cfg.Get("database.password")
    apiKey, _ := cfg.Get("api.key")
    dbURL, _ := cfg.Get("database.url")
    _ = dbPass // use to construct the database client; never log it
    _ = apiKey // use to construct the API client; never log it
    _ = dbURL
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
