package confii

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/confiify/confii-go/hook"
)

// selfConfigSecretPattern matches the placeholder grammar accepted by
// the in-root self-config secret hook: ${secret:key[:json_path][:version]}.
// The grammar intentionally matches the public [secret.Resolver] so a
// user can switch between imperative resolver wiring and a declarative
// `secrets:` self-config block without changing placeholder syntax.
var selfConfigSecretPattern = regexp.MustCompile(`\$\{secret:([^}:]+)(?::([^}:]*))?(?::([^}:]+))?\}`)

// SelfConfigSecretProviderFactory builds a [SelfConfigSecretStore]
// from the self-config `secrets:` block.
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
	rawProvider, _ := secrets["provider"].(string)
	provider := strings.ToLower(strings.TrimSpace(rawProvider))
	if provider == "" {
		return nil, &ConfigError{
			Op:  "ApplySelfConfig",
			Err: fmt.Errorf("%w: self-config `secrets:` block is missing a `provider` field", ErrConfigLoad),
		}
	}
	factory, ok := LookupSelfConfigSecretProvider(provider)
	if !ok {
		return nil, &ConfigError{
			Op: "ApplySelfConfig",
			Err: fmt.Errorf(
				"%w: unsupported self-config secrets provider %q (registered: %s); cloud providers must be opted in via a build-tagged blank import of secret/cloud",
				ErrConfigLoad, rawProvider, registeredSelfConfigProviderNames(),
			),
		}
	}
	store, err := factory(secrets)
	if err != nil {
		return nil, &ConfigError{
			Op:  "ApplySelfConfig",
			Err: fmt.Errorf("%w: self-config secrets provider %q failed to build: %w", ErrConfigLoad, rawProvider, err),
		}
	}
	return makeSelfConfigSecretHook(store), nil
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
	// Insertion-sort: tiny set, avoids importing sort just for this.
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
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
	return func(ctx context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok {
			return value, nil
		}
		if !strings.Contains(s, "${secret:") {
			return value, nil
		}

		var firstErr error
		result := selfConfigSecretPattern.ReplaceAllStringFunc(s, func(match string) string {
			if firstErr != nil {
				return match
			}
			groups := selfConfigSecretPattern.FindStringSubmatch(match)
			key := groups[1]
			val, err := store.GetSecret(ctx, key)
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
