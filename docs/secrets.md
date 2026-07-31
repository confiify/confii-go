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

Independent top-level branches resolve concurrently (default limit: four),
while duplicate provider/key/version requests are coalesced. Configure the
bound with `secret_resolution_concurrency` or
`WithSecretResolutionConcurrency`. Context cancellation always stops the
operation, regardless of `on_error`. Declaratively created providers that
implement `Close() error` are released by `Config.Close`.

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
if err := cfg.RefreshSecretsWithContext(ctx); err != nil {
    // The previous ready configuration remains active.
}
```

`Reload` performs the same eager materialization after rebuilding the source
layers. A failed provider read or validation leaves the prior configuration
active. Hooks must be supplied before construction with `confii.WithSecretHook`,
`confii.WithSecretResolver`, or the general construction-time hook options.
The plan is frozen after `New` succeeds and every access surface observes the
same published values. See [Hooks](hooks.md#runtime-read-behavior).

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
active environment. `secret@provider` is distinct from the colon-delimited
key, field, and version grammar, keeping provider routing unambiguous.

---

## Built-in Stores

### DictStore

In-memory store for testing and development. Supports versioning via `SetSecret`.

```go
import "github.com/confiify/confii-go/v2/secret"

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
import "github.com/confiify/confii-go/v2/secret"

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
import "github.com/confiify/confii-go/v2/secret"

multi := secret.NewMultiStore(
    []confii.SecretStore{vaultStore, awsStore, envStore},
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

### Optional store capabilities

`SecretStore` is the portable read/write contract. Applications can
feature-detect two additional capabilities without coupling themselves to a
specific provider:

```go
if checker, ok := store.(confii.SecretExistenceChecker); ok {
    exists, err := checker.SecretExists(ctx, "db/password")
    // Existence is checked without returning secret material.
}

if metadataProvider, ok := store.(confii.SecretMetadataProvider); ok {
    metadata, err := metadataProvider.GetSecretMetadata(ctx, "db/password")
    // Metadata must never contain the secret value.
}
```

Providers are not required to implement these interfaces. `DictStore`
implements both for local development and tests; cloud integrations may expose
them when the provider offers a value-safe operation. Applications must retain
the ordinary `GetSecret` path when the capability assertion is false.

---

## Cloud Stores

Cloud stores live in the separate `secret/cloud` module and require provider
build tags to compile. This keeps the binary small when you don't need them.

### AWS Secrets Manager

```bash
go build -tags aws
```

```go
import "github.com/confiify/confii-go/secret/cloud/v2"

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
import "github.com/confiify/confii-go/secret/cloud/v2"

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
import "github.com/confiify/confii-go/secret/cloud/v2"

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
import "github.com/confiify/confii-go/secret/cloud/v2"

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
`VaultOption`; all constructors return the vendor-neutral `VaultStore` type.

Field extraction uses the provider-neutral `WithField` option:

```go
// Fetch only the "password" field from secret/data/db/credentials
val, _ := store.GetSecret(ctx, "db/credentials", confii.WithField("password"))
```

---

## Vault Auth Methods

The Vault-compatible integration exposes adapters for nine authentication
methods. AppRole, Kubernetes, AWS IAM, Azure managed identity, and GCP use the
official HashiCorp Vault auth packages for credential discovery, signing, and
login payload construction. Token, LDAP, generic JWT, and interactive OIDC use
the Vault API directly because HashiCorp does not publish corresponding Go auth
helpers for those flows.

Pass one method via `WithVaultAuth`. CI live-tests Token and AppRole against
OpenBao and exercises every other adapter against protocol fixtures. Provider
identity, role, and trust configuration remains deployment-specific and must be
verified in the target environment:

=== "Token"

    ```go
    cloud.WithVaultAuth(&cloud.TokenAuth{
        Token: "hvs.xxxxx",
    })
    ```

=== "AppRole"

    ```go
    cloud.WithVaultAuth(&cloud.AppRoleAuth{
        RoleID:      "role-id",
        SecretIDEnv: "VAULT_SECRET_ID",
        MountPoint:  "approle",  // default: "approle"
    })
    ```

    Exactly one of `SecretID`, `SecretIDFile`, or `SecretIDEnv` is required.
    File and environment sources are read for each authentication attempt, so
    rotated credentials do not require rebuilding the store. Set
    `WrappingToken` when that source contains a Vault response-wrapping token.

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
        PasswordProvider: func(ctx context.Context) (string, error) {
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
        TokenPath:  "/var/run/secrets/kubernetes.io/serviceaccount/token",
        MountPoint: "kubernetes",  // default: "kubernetes"
    })
    ```

    Supply at most one of `JWT`, `TokenPath`, or `TokenEnv`. With none set, the
    official package reads the standard projected service-account token path.

=== "AWS IAM"

    ```go
    cloud.WithVaultAuth(&cloud.AWSIAMAuth{
        Role:              "my-aws-role",
        Region:            "us-east-1",
        IAMServerIDHeader: "vault.example.com", // optional role binding
        MountPoint:        "aws",  // default: "aws"
    })
    ```

    The official auth package discovers the standard AWS credential chain and
    signs the STS `GetCallerIdentity` request. Applications with an external
    signer can instead use `AWSIAMSignedRequestAuth` and provide Vault's four
    base64-encoded IAM request fields explicitly.

=== "Azure"

    ```go
    cloud.WithVaultAuth(&cloud.AzureAuth{
        Role:       "my-azure-role",
        Resource:   "https://management.azure.com/",  // optional audience
        MountPoint: "azure",  // default: "azure"
    })
    ```

    `AzureAuth` uses the official package to obtain a managed-identity token and
    instance metadata from Azure IMDS. Workload identities that already own a
    JWT can use the explicit `AzureJWTAuth` adapter.

=== "GCP"

    ```go
    cloud.WithVaultAuth(&cloud.GCPAuth{
        Role:                "my-gcp-role",
        AuthType:            "iam", // "gce" is the default
        ServiceAccountEmail: "app@project.iam.gserviceaccount.com",
        MountPoint:          "gcp",  // default: "gcp"
    })
    ```

    GCE mode obtains an identity JWT from the metadata service. IAM mode signs
    through IAM Credentials using application default credentials. An external
    identity JWT can use `JWTAuth` with `MountPoint: "gcp"`.

=== "OIDC"

    ```go
    cloud.WithVaultAuth(&cloud.OIDCAuth{
        Role:         "my-oidc-role",
        MountPoint:   "oidc",  // default: "oidc"
        RedirectURI:  "http://localhost:8250/oidc/callback",
    })
    ```

    OIDC starts a loopback callback server, opens the provider login in the default browser, validates the returned state and nonce, and exchanges the authorization code with Vault. The redirect URI must be allowed by both the Vault role and the OIDC provider. Embedded/headless applications can set `CallbackProvider` to collect and return the full callback URL themselves; `CallbackTimeout` and `OpenBrowser` customize the interactive flow.

`WithVaultAppRole` is shorthand for AppRole authentication:

```go
cloud.WithVaultAppRole("role-id", "secret-id")
```

---

## Declarative Self-Config Providers

Cloud stores can be wired through `.confii.yaml` when the application
blank-imports `github.com/confiify/confii-go/secret/cloud/v2` and builds with
the matching tag. Each tagged package registers its provider during `init`.
Confii v2 requires named providers, including when only one provider is used:

```yaml
secrets:
  default_provider: vault
  environment_defaults:
    production: aws-production
    analytics: gcp-analytics

  providers:
    vault:
      type: vault
      address: https://vault.internal:8200
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

Vault self-config accepts `token`, `approle`, `ldap`, `jwt`, `kubernetes`,
`aws_iam`, `azure`, `gcp`, and interactive `oidc`. The official-provider forms
also accept their provider-specific fields: AppRole secret ID sources,
Kubernetes token sources, AWS `region`, Azure `resource`, and GCP `auth_type`
plus `service_account_email`. Advanced external-identity forms are named
explicitly: `aws_signed_request`, `azure_jwt`, and `gcp_jwt`.

This is configuration support, not a claim that every provider identity is
turnkey or live-certified. Token and AppRole are the CI-tested OpenBao paths;
the others require provider-side identity configuration. `auth` may be a
method string with fields alongside it or a nested map with `method`. A root
`token` or `VAULT_TOKEN` is used for token auth. The same build can register
multiple providers by enabling multiple tags, for example `-tags="aws,vault"`.

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
import "github.com/confiify/confii-go/v2/secret"

resolver := secret.NewResolver(store,
    secret.WithCache(true),               // enable caching (default: true)
    secret.WithCacheTTL(5 * time.Minute), // cache expiration (0 = no expiry)
    secret.WithResolverPrefix("prod/"),   // prepend to all keys
)
```

| Option | Default | Description |
|---|---|---|
| `WithCache(bool)` | `true` | Enable/disable internal cache |
| `WithCacheTTL(duration)` | `0` (no expiry) | How long cached values are valid |
| `WithResolverPrefix(string)` | `""` | Prepended to all secret keys before lookup |

Missing references always return a typed error in v2; unresolved placeholders
are never published as configuration.

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

cfg, err := confii.NewWithContext[any](ctx,
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
    type casting, then constructor-time secret resolution. Register every hook
    through constructor options or the builder; the materialization plan is
    immutable after `New` succeeds.

---

## Multi-Store Fallback Chain

Combine multiple stores for environment-flexible secret resolution:

```go
package main

import (
    "context"
    "time"

    "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
    "github.com/confiify/confii-go/v2/secret"
    "github.com/confiify/confii-go/secret/cloud/v2"
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
        secret.    )

    // Resolver with caching
    resolver := secret.NewResolver(multi,
        secret.WithCache(true),
        secret.WithCacheTTL(10 * time.Minute),
    )

    // Load, consolidate, and resolve before returning.
    cfg, err := confii.NewWithContext[any](ctx,
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

    "github.com/confiify/confii-go/v2"
    "github.com/confiify/confii-go/v2/loader"
    "github.com/confiify/confii-go/v2/secret"
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
        secret.    )

    // Load, resolve all effective references, and validate before returning.
    cfg, err := confii.NewWithContext[any](ctx,
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
