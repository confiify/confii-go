package loader

import (
	"context"
	"os"
	"strings"

	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/internal/typecoerce"
)

// EnvironmentLoader loads configuration from environment variables matching a prefix.
// Variables are stripped of the prefix, split on the separator to create nested keys,
// and lowercased.
type EnvironmentLoader struct {
	prefix    string
	separator string
}

// NewEnvironment creates a new environment variable loader.
// The prefix is uppercased automatically. The default separator is "__".
func NewEnvironment(prefix string, opts ...EnvLoaderOption) *EnvironmentLoader {
	l := &EnvironmentLoader{
		prefix:    strings.ToUpper(prefix),
		separator: "__",
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// EnvLoaderOption configures the EnvironmentLoader.
type EnvLoaderOption func(*EnvironmentLoader)

// WithSeparator sets the nesting separator (default "__").
func WithSeparator(sep string) EnvLoaderOption {
	return func(l *EnvironmentLoader) { l.separator = sep }
}

// Source returns the identifier for this loader's configuration source.
func (l *EnvironmentLoader) Source() string {
	return "environment:" + l.prefix
}

// Load reads environment variables matching the configured prefix and parses them into a nested configuration map.
func (l *EnvironmentLoader) Load(_ context.Context) (map[string]any, error) {
	envPrefix := l.prefix + "_"
	result := make(map[string]any)

	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(key, envPrefix) {
			continue
		}

		// Strip prefix and leading underscore.
		key = strings.TrimPrefix(key, envPrefix)

		// Split on separator to create nested keys, lowercase all parts.
		parts := strings.Split(key, l.separator)
		for i := range parts {
			parts[i] = strings.ToLower(parts[i])
		}

		parsed := typecoerce.ParseScalar(value, false)

		// Build nested path using dot notation.
		keyPath := strings.Join(parts, ".")
		_ = dictutil.SetNested(result, keyPath, parsed)
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
