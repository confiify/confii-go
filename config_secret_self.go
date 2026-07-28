// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/confiify/confii-go/hook"
)

// selfConfigSecretPattern matches both the backward-compatible default
// provider form and an explicitly routed named-provider form:
//
//	${secret:key[:json_path][:version]}
//	${secret@provider:key[:json_path][:version]}
//
// The unqualified grammar intentionally matches the public [secret.Resolver]
// so a user can switch from imperative resolver wiring without changing
// placeholders. The @provider qualifier is the declarative routing extension.
var selfConfigSecretPattern = regexp.MustCompile(`\$\{secret(?:@([A-Za-z0-9][A-Za-z0-9_.-]*))?:([^}:]+)(?::([^}:]*))?(?::([^}:]+))?\}`)

// SelfConfigSecretProviderFactory builds a [SelfConfigSecretStore]
// from the legacy self-config `secrets:` block or one entry under the named
// `secrets.providers` map.
//
// Built-in providers ("env", "dict", "file") register at init time.
// External providers — typically the build-tag-gated cloud secret
// stores under [secret/cloud] — register themselves at import via
// [RegisterSelfConfigSecretProvider]. The pattern follows
// [database/sql] driver registration: a blank import of the cloud
// subpackage with the appropriate build tag installs its provider.
type SelfConfigSecretProviderFactory func(cfg map[string]any) (SelfConfigSecretStore, error)

var selfConfigSecretProviders sync.Map // map[string]SelfConfigSecretProviderFactory

// RegisterSelfConfigSecretProvider registers a factory under the
// given case-insensitive provider name. A subsequent registration
// under the same name overwrites the previous entry.
func RegisterSelfConfigSecretProvider(name string, factory SelfConfigSecretProviderFactory) {
	if factory == nil {
		return
	}
	selfConfigSecretProviders.Store(strings.ToLower(strings.TrimSpace(name)), factory)
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
	RegisterSelfConfigSecretProvider("env", func(cfg map[string]any) (SelfConfigSecretStore, error) {
		return newSelfConfigEnvStore(cfg), nil
	})
	RegisterSelfConfigSecretProvider("dict", func(cfg map[string]any) (SelfConfigSecretStore, error) {
		return newSelfConfigDictStore(cfg)
	})
	RegisterSelfConfigSecretProvider("file", func(cfg map[string]any) (SelfConfigSecretStore, error) {
		return newSelfConfigFileStore(cfg)
	})
}

// buildSelfConfigSecretHook constructs a context-aware [hook.FuncCtx]
// that resolves ${secret:...} placeholders against the provider
// declared in a self-config `secrets:` block. Provider names are
// resolved through the [RegisterSelfConfigSecretProvider] registry.
// The hook fails fast on a missing or store-erroring secret, matching
// the public secret.Resolver's FailOnMissing(true) default.
func buildSelfConfigSecretHook(secrets map[string]any) (hook.FuncCtx, error) {
	h, _, _, err := buildSelfConfigSecretHookForEnvironment(secrets, "")
	return h, err
}

// buildSelfConfigSecretHookForEnvironment supports both the legacy
// single-provider shape and named providers with an environment-selected
// default. Provider factories are validated at startup but initialized lazily
// on first use, so an environment does not need credentials for unrelated
// providers.
func buildSelfConfigSecretHookForEnvironment(secrets map[string]any, environment string) (hook.FuncCtx, string, []string, error) {
	if _, named := secrets["providers"]; named {
		return buildNamedSelfConfigSecretHook(secrets, environment)
	}
	rawProvider, _ := secrets["provider"].(string)
	provider := strings.ToLower(strings.TrimSpace(rawProvider))
	if provider == "" {
		return nil, "", nil, &ConfigError{
			Op:  "ApplySelfConfig",
			Err: fmt.Errorf("%w: self-config `secrets:` block is missing a `provider` field", ErrConfigLoad),
		}
	}
	factory, ok := LookupSelfConfigSecretProvider(provider)
	if !ok {
		return nil, "", nil, &ConfigError{
			Op: "ApplySelfConfig",
			Err: fmt.Errorf(
				"%w: unsupported self-config secrets provider %q (registered: %s); cloud providers must be opted in via a build-tagged blank import of secret/cloud",
				ErrConfigLoad, rawProvider, registeredSelfConfigProviderNames(),
			),
		}
	}
	store, err := factory(secrets)
	if err != nil {
		return nil, "", nil, &ConfigError{
			Op:  "ApplySelfConfig",
			Err: fmt.Errorf("%w: self-config secrets provider %q failed to build: %w", ErrConfigLoad, rawProvider, err),
		}
	}
	return makeSelfConfigSecretHook(store), provider, []string{provider}, nil
}

type selfConfigProviderSpec struct {
	providerType string
	config       map[string]any
	factory      SelfConfigSecretProviderFactory
	once         sync.Once
	store        SelfConfigSecretStore
	err          error
}

type selfConfigSecretRouter struct {
	defaultProvider string
	providers       map[string]*selfConfigProviderSpec
}

func buildNamedSelfConfigSecretHook(secrets map[string]any, environment string) (hook.FuncCtx, string, []string, error) {
	if _, legacy := secrets["provider"]; legacy {
		return nil, "", nil, selfConfigSecretConfigError("`provider` and `providers` cannot be combined")
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
		for _, field := range []string{"type", "provider"} {
			if rawType, exists := cfg[field]; exists {
				if value, ok := rawType.(string); !ok || strings.TrimSpace(value) == "" {
					return nil, "", nil, selfConfigSecretConfigError(fmt.Sprintf("provider %q `%s` must be a non-empty string", rawName, field))
				}
			}
		}
		providerType := strings.ToLower(strings.TrimSpace(firstString(cfg, "type", "provider")))
		if providerType == "" {
			providerType = name
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
	return &ConfigError{Op: "ApplySelfConfig", Err: fmt.Errorf("%w: self-config secrets: %s", ErrConfigLoad, message)}
}

func (r *selfConfigSecretRouter) get(ctx context.Context, provider, key, version string) (any, error) {
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
	spec.once.Do(func() {
		spec.store, spec.err = spec.factory(spec.config)
		if spec.err == nil && spec.store == nil {
			spec.err = errors.New("provider factory returned a nil store")
		}
	})
	if spec.err != nil {
		return nil, fmt.Errorf("%w: secret provider %q (%s) failed to initialize: %v", ErrSecretAccess, provider, spec.providerType, spec.err)
	}
	return getSelfConfigSecret(ctx, spec.store, key, version)
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

// SelfConfigSecretStore is the read-only interface a self-config
// secret provider must satisfy. It is narrower than [SecretStore]
// because the configuration access path only ever reads — no
// existence check, metadata, list, set, or delete. External
// providers registered via [RegisterSelfConfigSecretProvider]
// satisfy this interface directly.
type SelfConfigSecretStore interface {
	GetSecret(ctx context.Context, key string) (any, error)
}

// SelfConfigSecretRequest carries the complete declarative placeholder
// request. Version-capable providers may implement
// SelfConfigSecretRequestStore; legacy providers remain source compatible and
// continue to satisfy SelfConfigSecretStore.
type SelfConfigSecretRequest struct {
	Key     string
	Version string
}

// SelfConfigSecretRequestStore is the optional version-aware extension used
// by declarative secret resolution.
type SelfConfigSecretRequestStore interface {
	GetSecretRequest(ctx context.Context, request SelfConfigSecretRequest) (any, error)
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

func (s *selfConfigEnvStore) GetSecret(_ context.Context, key string) (any, error) {
	envKey := s.envKey(key)
	val, ok := os.LookupEnv(envKey)
	if !ok {
		return nil, fmt.Errorf("%w: env var %s not found", ErrSecretNotFound, envKey)
	}
	return val, nil
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

func (s *selfConfigDictStore) GetSecret(_ context.Context, key string) (any, error) {
	v, ok := s.entries[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSecretNotFound, key)
	}
	return v, nil
}

// selfConfigFileStore reads secret values from files on disk, the
// shape used by Docker secret-mounts and Kubernetes Secret volumes
// (e.g. /run/secrets/<name>). Each `${secret:KEY}` placeholder maps
// to base_dir + KEY + extension; the file's full content is returned,
// with trailing whitespace trimmed when trim_whitespace is true
// (default). Keys containing path traversal sequences are rejected
// in [selfConfigFileStore.GetSecret].
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

func (s *selfConfigFileStore) GetSecret(_ context.Context, key string) (any, error) {
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
	return str, nil
}

// makeSelfConfigSecretHook returns a [hook.FuncCtx] that performs
// in-place ${secret:...} placeholder substitution against store. The
// first missing or store-erroring placeholder aborts substitution
// and the error propagates back through [hook.Processor.ProcessCtx]
// to the caller of [Config.GetCtx]. Strings without any placeholder
// are returned unchanged with no store traffic.
func makeSelfConfigSecretHook(store SelfConfigSecretStore) hook.FuncCtx {
	return makeSelfConfigSecretValueHook(func(ctx context.Context, _ string, key, version string) (any, error) {
		return getSelfConfigSecret(ctx, store, key, version)
	})
}

func makeSelfConfigRoutedSecretHook(router *selfConfigSecretRouter) hook.FuncCtx {
	return makeSelfConfigSecretValueHook(router.get)
}

func getSelfConfigSecret(ctx context.Context, store SelfConfigSecretStore, key, version string) (any, error) {
	if requestStore, ok := store.(SelfConfigSecretRequestStore); ok {
		return requestStore.GetSecretRequest(ctx, SelfConfigSecretRequest{Key: key, Version: version})
	}
	if version != "" {
		return nil, fmt.Errorf("%w: provider does not support versioned declarative reads", ErrSecretValidation)
	}
	return store.GetSecret(ctx, key)
}

type selfConfigSecretGetter func(context.Context, string, string, string) (any, error)

func makeSelfConfigSecretValueHook(get selfConfigSecretGetter) hook.FuncCtx {
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
			val, err := get(ctx, provider, key, version)
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
