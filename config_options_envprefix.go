package confii

import (
	"context"
	"os"
	"strings"

	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/internal/typecoerce"
)

// envPrefixAutoLoader is the [Loader] auto-installed by [WithEnvPrefix] so
// that environment variables matching the prefix participate in the normal
// load+merge pipeline (Wave 13 G03). It is a behavioral mirror of
// [loader.EnvironmentLoader] with the canonical "__" double-underscore
// nesting separator, intentionally re-implemented in the root package
// because the public `loader` subpackage imports `confii` (so the root
// cannot import it back without an import cycle).
//
// The Source() identifier is deliberately the same string format
// ("environment:<UPPERCASE_PREFIX>") used by loader.EnvironmentLoader
// so that a user-supplied loader.NewEnvironment(prefix) and the loader
// auto-appended by WithEnvPrefix(prefix) collide on identity, allowing
// the option closure to dedupe and avoid double-application.
type envPrefixAutoLoader struct {
	prefix string
}

// Source returns the identifier for this loader's configuration source.
// The format intentionally matches loader.EnvironmentLoader.Source() so
// users supplying their own loader.NewEnvironment(prefix) via
// WithLoaders are detected as duplicates and not double-applied.
func (l *envPrefixAutoLoader) Source() string {
	return "environment:" + l.prefix
}

// Load reads environment variables matching the configured prefix and
// parses them into a nested configuration map using the same rules as
// loader.EnvironmentLoader: the prefix is followed by "_", the remaining
// key is split on "__" to produce nested segments, each segment is
// lowercased, and string values are parsed via typecoerce.ParseScalar.
func (l *envPrefixAutoLoader) Load(_ context.Context) (map[string]any, error) {
	envPrefix := l.prefix + "_"
	const separator = "__"
	result := make(map[string]any)

	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(key, envPrefix) {
			continue
		}

		// Strip prefix (and the trailing underscore that joins prefix to body).
		key = strings.TrimPrefix(key, envPrefix)

		// Split on separator to create nested keys, lowercase all parts.
		parts := strings.Split(key, separator)
		for i := range parts {
			parts[i] = strings.ToLower(parts[i])
		}

		parsed := typecoerce.ParseScalar(value, false)

		keyPath := strings.Join(parts, ".")
		_ = dictutil.SetNested(result, keyPath, parsed)
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
