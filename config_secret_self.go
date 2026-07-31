// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/confiify/confii-go/v2/hook"
)

// selfConfigSecretPattern matches default-provider and explicitly routed forms:
//
//	${secret:key[:json_path][:version]}
//	${secret@provider:key[:json_path][:version]}
//
// The unqualified grammar intentionally matches the public [secret.Resolver]
// so a user can switch from imperative resolver wiring without changing
// placeholders. The @provider qualifier is the declarative routing extension.
var selfConfigSecretPattern = regexp.MustCompile(`\$\{secret(?:@([A-Za-z0-9][A-Za-z0-9_.-]*))?:([^}:]+)(?::([^}:]*))?(?::([^}:]+))?\}`)

// SelfConfigSecretProviderFactory builds a [SecretReader] from one
// entry under the `secrets.providers` map.
//
// Built-in providers ("env", "dict", "file") register at init time.
// External providers — typically the build-tag-gated cloud secret
// stores under [secret/cloud] — register themselves at import via
// [RegisterSelfConfigSecretProvider]. The pattern follows
// [database/sql] driver registration: a blank import of the cloud
// subpackage with the appropriate build tag installs its provider.
type SelfConfigSecretProviderFactory func(context.Context, map[string]any) (SecretReader, error)

var selfConfigSecretProviders sync.Map // map[string]SelfConfigSecretProviderFactory

// RegisterSelfConfigSecretProvider registers a factory under the given
// case-insensitive provider name. It panics for an empty name, a nil factory,
// or a duplicate normalized name so initialization order cannot silently
// replace the implementation selected for a provider alias.
func RegisterSelfConfigSecretProvider(name string, factory SelfConfigSecretProviderFactory) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		panic("confii: RegisterSelfConfigSecretProvider called with empty name")
	}
	if factory == nil {
		panic("confii: RegisterSelfConfigSecretProvider called with nil factory for " + name)
	}
	if _, loaded := selfConfigSecretProviders.LoadOrStore(name, factory); loaded {
		panic("confii: RegisterSelfConfigSecretProvider called twice for " + name)
	}
}

// LookupSelfConfigSecretProvider returns the factory registered
// under name (case-insensitive) and a boolean reporting whether it
// was found. Production code paths reach the registry indirectly
// through buildSelfConfigSecretHook; this entry point is exposed
// primarily for tests and introspection.
func LookupSelfConfigSecretProvider(name string) (SelfConfigSecretProviderFactory, bool) {
	v, ok := selfConfigSecretProviders.Load(strings.ToLower(strings.TrimSpace(name)))
	if !ok {
		return nil, false
	}
	return v.(SelfConfigSecretProviderFactory), true
}

func init() {
	RegisterSelfConfigSecretProvider("env", func(_ context.Context, cfg map[string]any) (SecretReader, error) {
		return newSelfConfigEnvStore(cfg), nil
	})
	RegisterSelfConfigSecretProvider("dict", func(_ context.Context, cfg map[string]any) (SecretReader, error) {
		return newSelfConfigDictStore(cfg)
	})
	RegisterSelfConfigSecretProvider("file", func(_ context.Context, cfg map[string]any) (SecretReader, error) {
		return newSelfConfigFileStore(cfg)
	})
}

// buildSelfConfigSecretHook constructs a context-aware [hook.Func]
// that resolves ${secret:...} placeholders against the provider
// declared in a self-config `secrets:` block. Provider names are
// resolved through the [RegisterSelfConfigSecretProvider] registry.
// The hook fails fast on a missing or store-erroring secret, matching
// the public secret.Resolver's FailOnMissing(true) default.
func buildSelfConfigSecretHook(ctx context.Context, secrets map[string]any) (hook.Func, error) {
	h, _, _, err := buildSelfConfigSecretHookForEnvironment(ctx, secrets, "")
	return h, err
}

// buildSelfConfigSecretHookForEnvironment supports named providers with an
// environment-selected default. Provider factories are validated at startup but initialized lazily
// on first use, so an environment does not need credentials for unrelated
// providers.
func buildSelfConfigSecretHookForEnvironment(_ context.Context, secrets map[string]any, environment string) (hook.Func, string, []string, error) {
	return buildNamedSelfConfigSecretHook(secrets, environment)
}

type selfConfigProviderSpec struct {
	providerType string
	config       map[string]any
	factory      SelfConfigSecretProviderFactory
	mu           sync.Mutex
	store        SecretReader
}

type selfConfigSecretRouter struct {
	defaultProvider string
	providers       map[string]*selfConfigProviderSpec
}

type selfConfigResourceCollectorContextKey struct{}

type selfConfigResourceCollector struct {
	mu        sync.Mutex
	resources []any
	closed    bool
}

func newSelfConfigResourceCollector() *selfConfigResourceCollector {
	return &selfConfigResourceCollector{}
}

func (c *selfConfigResourceCollector) add(resource any) bool {
	if c == nil || resource == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.resources = append(c.resources, resource)
	return true
}

func (c *selfConfigResourceCollector) closeAndSnapshot() []any {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	resources := append([]any(nil), c.resources...)
	c.resources = nil
	return resources
}

func collectSelfConfigResource(ctx context.Context, resource any) bool {
	if collector, ok := ctx.Value(selfConfigResourceCollectorContextKey{}).(*selfConfigResourceCollector); ok {
		return collector.add(resource)
	}
	return true
}

func (c *Config[T]) withManagedResourceContext(ctx context.Context) context.Context {
	if ctx == nil || c == nil || c.resourceRegistry == nil {
		return ctx
	}
	if existing, ok := ctx.Value(selfConfigResourceCollectorContextKey{}).(*selfConfigResourceCollector); ok && existing == c.resourceRegistry {
		return ctx
	}
	return context.WithValue(ctx, selfConfigResourceCollectorContextKey{}, c.resourceRegistry)
}

func buildNamedSelfConfigSecretHook(secrets map[string]any, environment string) (hook.Func, string, []string, error) {
	for key := range secrets {
		switch key {
		case "providers", "default_provider", "environment_defaults":
		default:
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("unsupported secrets field %q", key))
		}
	}
	rawProviders, ok := secrets["providers"].(map[string]any)
	if !ok || len(rawProviders) == 0 {
		return nil, "", nil, selfConfigSecretConfigError("`providers` must be a non-empty map")
	}
	router := &selfConfigSecretRouter{providers: make(map[string]*selfConfigProviderSpec, len(rawProviders))}
	names := make([]string, 0, len(rawProviders))
	for rawName, rawSpec := range rawProviders {
		name := normalizeSelfConfigProviderAlias(rawName)
		if name == "" {
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("invalid provider alias %q", rawName))
		}
		if _, duplicate := router.providers[name]; duplicate {
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("provider alias %q is duplicated after case normalization", rawName))
		}
		cfg, ok := rawSpec.(map[string]any)
		if !ok {
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("provider %q must be a map", rawName))
		}
		if _, legacyAlias := cfg["provider"]; legacyAlias {
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("provider %q uses removed `provider`; use `type`", rawName))
		}
		rawType, exists := cfg["type"]
		providerType, validType := rawType.(string)
		providerType = strings.ToLower(strings.TrimSpace(providerType))
		if !exists || !validType || providerType == "" {
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("provider %q requires a non-empty `type`", rawName))
		}
		factory, ok := LookupSelfConfigSecretProvider(providerType)
		if !ok {
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("provider %q uses unsupported type %q (registered: %s)", rawName, providerType, registeredSelfConfigProviderNames()))
		}
		router.providers[name] = &selfConfigProviderSpec{providerType: providerType, config: maps.Clone(cfg), factory: factory}
		names = append(names, name)
	}
	sort.Strings(names)

	if rawDefault, exists := secrets["default_provider"]; exists {
		if value, ok := rawDefault.(string); !ok || strings.TrimSpace(value) == "" {
			return nil, "", nil, selfConfigSecretConfigError("`default_provider` must be a non-empty provider alias")
		}
	}
	rawDefaultProvider := firstString(secrets, "default_provider")
	defaultProvider := normalizeSelfConfigProviderAlias(rawDefaultProvider)
	if rawDefaultProvider != "" && defaultProvider == "" {
		return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("invalid default_provider alias %q", rawDefaultProvider))
	}
	if rawDefaults, exists := secrets["environment_defaults"]; exists {
		defaults, ok := rawDefaults.(map[string]any)
		if !ok {
			return nil, "", nil, selfConfigSecretConfigError("`environment_defaults` must be a map")
		}
		for envName, rawValue := range defaults {
			value, ok := rawValue.(string)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("environment default for %q must be a non-empty provider alias", envName))
			}
			alias := normalizeSelfConfigProviderAlias(value)
			if alias == "" {
				return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("invalid provider alias %q for environment %q", value, envName))
			}
			if _, declared := router.providers[alias]; !declared {
				return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("environment default %q for %q is not declared under `providers`", alias, envName))
			}
		}
		if rawValue, exists := defaults[environment]; exists {
			value := rawValue.(string) // validated above
			defaultProvider = normalizeSelfConfigProviderAlias(value)
		}
	}
	if defaultProvider != "" {
		if _, ok := router.providers[defaultProvider]; !ok {
			return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("default provider %q is not declared under `providers`", defaultProvider))
		}
	}
	router.defaultProvider = defaultProvider
	return makeSelfConfigRoutedSecretHook(router), defaultProvider, names, nil
}

func normalizeSelfConfigProviderAlias(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	for i, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (i > 0 && (r == '_' || r == '-' || r == '.'))
		if !valid {
			return ""
		}
	}
	return value
}

func firstString(cfg map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := cfg[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func selfConfigSecretConfigError(message string) error {
	return &ConfigError{Op: "ApplySelfConfig", Code: ConfigErrorCodeLoad, Err: fmt.Errorf("self-config secrets: %s", message)}
}

func (r *selfConfigSecretRouter) get(ctx context.Context, provider, key, field, version string) (any, error) {
	if provider == "" {
		provider = r.defaultProvider
	}
	if provider == "" {
		return nil, fmt.Errorf("%w: unqualified secret reference %q has no default_provider", ErrSecretAccess, key)
	}
	spec, ok := r.providers[provider]
	if !ok {
		return nil, fmt.Errorf("%w: secret provider alias %q is not configured", ErrSecretAccess, provider)
	}
	spec.mu.Lock()
	store := spec.store
	if store == nil {
		var initErr error
		store, initErr = spec.factory(ctx, spec.config)
		if initErr == nil && store == nil {
			initErr = errors.New("provider factory returned a nil store")
		}
		if initErr != nil {
			spec.mu.Unlock()
			return nil, fmt.Errorf("%w: secret provider %q (%s) failed to initialize: %w", ErrSecretAccess, provider, spec.providerType, initErr)
		}
		if !collectSelfConfigResource(ctx, store) {
			if closer, ok := store.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
			spec.mu.Unlock()
			return nil, fmt.Errorf("%w: secret provider %q initialized after configuration close", ErrSecretAccess, provider)
		}
		spec.store = store
	}
	spec.mu.Unlock()
	return getSelfConfigSecret(ctx, store, key, field, version)
}

// registeredSelfConfigProviderNames returns a comma-separated list of
// currently-registered provider names, sorted lexicographically. Used
// in error messages so operators see the actual options available in
// their build configuration (which differs depending on cloud build
// tags).
func registeredSelfConfigProviderNames() string {
	var names []string
	selfConfigSecretProviders.Range(func(k, _ any) bool {
		if s, ok := k.(string); ok {
			names = append(names, s)
		}
		return true
	})
	if len(names) == 0 {
		return "(none)"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// SecretRequest describes one backend-neutral declarative secret read. Key is
// required; Field and Version are optional selectors whose interpretation is
// defined by the provider. Providers must return an explicit error when a
// requested selector is unsupported.
type SecretRequest struct {
	// Key identifies the secret within the selected provider.
	Key string
	// Field selects a member from a structured secret value.
	Field string
	// Version selects a backend-defined historical or immutable version.
	Version string
}

// SecretReader is the required contract for self-configured secret providers.
// Implementations must honor ctx, be safe for concurrent calls made by
// Confii's resolution workers, and distinguish genuine absence with
// [ErrSecretNotFound] from authentication, authorization, transport, and
// backend failures.
type SecretReader interface {
	// ReadSecret resolves request or returns a classified error.
	ReadSecret(ctx context.Context, request SecretRequest) (any, error)
}

// selfConfigEnvStore is the OS-environment-variable backed built-in
// provider. It lives in root rather than under [secret] because the
// secret/* subpackages import root, which would create a cycle.
// Behavior mirrors [secret.EnvStore].
type selfConfigEnvStore struct {
	prefix       string
	suffix       string
	transformKey bool
}

func newSelfConfigEnvStore(cfg map[string]any) *selfConfigEnvStore {
	s := &selfConfigEnvStore{transformKey: true}
	if p, ok := cfg["prefix"].(string); ok {
		s.prefix = p
	}
	if sx, ok := cfg["suffix"].(string); ok {
		s.suffix = sx
	}
	if tk, ok := cfg["transform_key"].(bool); ok {
		s.transformKey = tk
	}
	return s
}

func (s *selfConfigEnvStore) ReadSecret(_ context.Context, request SecretRequest) (any, error) {
	envKey := s.envKey(request.Key)
	val, ok := os.LookupEnv(envKey)
	if !ok {
		return nil, fmt.Errorf("%w: env var %s not found", ErrSecretNotFound, envKey)
	}
	return extractSelfConfigSecretPath(val, request.Field)
}

func (s *selfConfigEnvStore) envKey(key string) string {
	if s.transformKey {
		key = strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(key)
		key = strings.ToUpper(key)
	}
	return s.prefix + key + s.suffix
}

// selfConfigDictStore is an in-memory secret store backed by a map
// declared inline in self-config. It mirrors a read-only subset of
// [secret.DictStore] for tests, examples, and local-development
// scenarios that want declarative wiring without a real backend.
type selfConfigDictStore struct {
	entries map[string]any
}

func newSelfConfigDictStore(cfg map[string]any) (*selfConfigDictStore, error) {
	raw, ok := cfg["entries"]
	if !ok {
		return &selfConfigDictStore{entries: map[string]any{}}, nil
	}
	switch m := raw.(type) {
	case map[string]any:
		entries := make(map[string]any, len(m))
		maps.Copy(entries, m)
		return &selfConfigDictStore{entries: entries}, nil
	case map[any]any:
		entries := make(map[string]any, len(m))
		for k, v := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("dict provider: entries keys must be strings, got %T", k)
			}
			entries[ks] = v
		}
		return &selfConfigDictStore{entries: entries}, nil
	default:
		return nil, fmt.Errorf("dict provider: entries must be a map, got %T", raw)
	}
}

func (s *selfConfigDictStore) ReadSecret(_ context.Context, request SecretRequest) (any, error) {
	v, ok := s.entries[request.Key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSecretNotFound, request.Key)
	}
	return extractSelfConfigSecretPath(v, request.Field)
}

// selfConfigFileStore reads secret values from files on disk, the
// shape used by Docker secret-mounts and Kubernetes Secret volumes
// (e.g. /run/secrets/<name>). Each `${secret:KEY}` placeholder maps
// to base_dir + KEY + extension; the file's full content is returned,
// with trailing whitespace trimmed when trim_whitespace is true
// (default). Keys containing path traversal sequences are rejected
// in [selfConfigFileStore.ReadSecret].
type selfConfigFileStore struct {
	baseDir        string
	extension      string
	trimWhitespace bool
}

func newSelfConfigFileStore(cfg map[string]any) (*selfConfigFileStore, error) {
	s := &selfConfigFileStore{trimWhitespace: true}
	if v, ok := cfg["base_dir"].(string); ok {
		s.baseDir = v
	}
	if s.baseDir == "" {
		return nil, fmt.Errorf("file provider: `base_dir` is required")
	}
	if v, ok := cfg["extension"].(string); ok {
		s.extension = v
	}
	if v, ok := cfg["trim_whitespace"].(bool); ok {
		s.trimWhitespace = v
	}
	return s, nil
}

func (s *selfConfigFileStore) ReadSecret(_ context.Context, request SecretRequest) (any, error) {
	key := request.Key
	if strings.ContainsAny(key, "\x00") {
		return nil, fmt.Errorf("%w: invalid secret key %q (path traversal rejected)", ErrSecretNotFound, key)
	}

	// os.Root confines the open to baseDir even when a nested component is a
	// symlink. Lexical filepath checks alone are vulnerable to symlink races.
	root, err := os.OpenRoot(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("file provider: open root %s: %w", s.baseDir, err)
	}
	defer func() { _ = root.Close() }()

	relativePath := key + s.extension
	file, err := root.Open(relativePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s (file %s)", ErrSecretNotFound, key, relativePath)
		}
		return nil, fmt.Errorf("file provider: securely open %s: %w", relativePath, err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("file provider: read %s: %w", relativePath, err)
	}
	str := string(data)
	if s.trimWhitespace {
		str = strings.TrimSpace(str)
	}
	return extractSelfConfigSecretPath(str, request.Field)
}

// makeSelfConfigSecretHook returns a [hook.Func] that performs
// ${secret:...} placeholder substitution against store. The
// first missing or store-erroring placeholder aborts substitution
// and the error propagates to the snapshot-building operation. Strings without
// a placeholder are returned unchanged with no store traffic.
func makeSelfConfigSecretHook(store SecretReader) hook.Func {
	return makeSelfConfigSecretValueHook(func(ctx context.Context, _ string, key, field, version string) (any, error) {
		return getSelfConfigSecret(ctx, store, key, field, version)
	})
}

func makeSelfConfigRoutedSecretHook(router *selfConfigSecretRouter) hook.Func {
	return makeSelfConfigSecretValueHook(router.get)
}

func getSelfConfigSecret(ctx context.Context, store SecretReader, key, field, version string) (any, error) {
	return store.ReadSecret(ctx, SecretRequest{Key: key, Field: field, Version: version})
}

type selfConfigSecretGetter func(context.Context, string, string, string, string) (any, error)

// secretResolutionSession deduplicates provider reads during one eager
// materialization pass. A single remote secret document can feed multiple
// configuration keys or JSON paths without generating repeated backend
// requests. Entries are scoped to the caller's context and are never reused
// by a later explicit refresh.
type secretResolutionSession struct {
	mu      sync.Mutex
	entries map[string]*secretResolutionSessionEntry
}

type secretResolutionSessionEntry struct {
	done  chan struct{}
	value any
	err   error
}

type secretResolutionSessionContextKey struct{}

func withSecretResolutionSession(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, secretResolutionSessionContextKey{}, &secretResolutionSession{
		entries: make(map[string]*secretResolutionSessionEntry),
	})
}

func getSelfConfigSecretOnce(
	ctx context.Context,
	get selfConfigSecretGetter,
	provider, key, field, version string,
) (any, error) {
	session, ok := ctx.Value(secretResolutionSessionContextKey{}).(*secretResolutionSession)
	if !ok || session == nil {
		return get(ctx, provider, key, field, version)
	}
	// Fetch and coalesce the complete provider document independently of Field.
	// The router extracts each requested field after the shared read completes.
	cacheKey := provider + "\x00" + key + "\x00" + version
	session.mu.Lock()
	if existing, found := session.entries[cacheKey]; found {
		session.mu.Unlock()
		select {
		case <-existing.done:
			return existing.value, existing.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	entry := &secretResolutionSessionEntry{done: make(chan struct{})}
	session.entries[cacheKey] = entry
	session.mu.Unlock()

	entry.value, entry.err = get(ctx, provider, key, "", version)
	close(entry.done)
	return entry.value, entry.err
}

func makeSelfConfigSecretValueHook(get selfConfigSecretGetter) hook.Func {
	return func(ctx context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok {
			return value, nil
		}
		if !strings.Contains(s, "${secret") {
			return value, nil
		}

		var firstErr error
		result := selfConfigSecretPattern.ReplaceAllStringFunc(s, func(match string) string {
			if firstErr != nil {
				return match
			}
			groups := selfConfigSecretPattern.FindStringSubmatch(match)
			provider := strings.ToLower(groups[1])
			key := groups[2]
			jsonPath := groups[3]
			version := groups[4]
			val, err := getSelfConfigSecretOnce(ctx, get, provider, key, jsonPath, version)
			if err != nil {
				firstErr = err
				return match
			}
			val, err = extractSelfConfigSecretPath(val, jsonPath)
			if err != nil {
				firstErr = err
				return match
			}
			return fmt.Sprintf("%v", val)
		})
		if firstErr != nil {
			// Fail-fast: return the original value (not partial result)
			// alongside the error, matching secret.Resolver's contract.
			return value, firstErr
		}
		return result, nil
	}
}

func extractSelfConfigSecretPath(value any, path string) (any, error) {
	if path == "" {
		return value, nil
	}
	if encoded, ok := value.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			return nil, fmt.Errorf("%w: cannot traverse path %q in non-map secret value", ErrSecretValidation, path)
		}
		value = decoded
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: cannot traverse path %q in non-map secret value", ErrSecretValidation, path)
		}
		current, ok = mapping[part]
		if !ok {
			return nil, fmt.Errorf("%w: path %q not found in secret", ErrSecretValidation, path)
		}
	}
	return current, nil
}
