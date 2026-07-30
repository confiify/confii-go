// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"fmt"
	"os"
	"strings"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/internal/typecoerce"
)

// EnvironmentLoader loads process variables named PREFIX_KEY. PREFIX is
// uppercased, key components are split by the configured separator and
// lowercased, and values are conservatively converted to bool, int, or float.
// For example, NewEnvironment("TODO") maps TODO_SERVER__PORT=8080 to
// server.port with the default "__" separator.
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

// WithSeparator sets the nesting separator. The default is "__". An empty
// separator is rejected by Load; callers should provide a stable separator
// that cannot occur within key parts.
func WithSeparator(sep string) EnvLoaderOption {
	return func(l *EnvironmentLoader) { l.separator = sep }
}

// Source returns the identifier for this loader's configuration source.
func (l *EnvironmentLoader) Source() string {
	return "environment:" + l.prefix
}

// Load snapshots matching process variables into a new nested map. It returns
// nil, nil when none match. The context is accepted for Loader compatibility;
// reading the process environment is synchronous and does not observe
// cancellation.
func (l *EnvironmentLoader) Load(_ context.Context) (map[string]any, error) {
	if l.separator == "" {
		return nil, confii.NewLoadError(l.Source(), fmt.Errorf("environment separator must not be empty"))
	}
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
		if err := dictutil.SetNested(result, keyPath, parsed); err != nil {
			return nil, confii.NewLoadError(l.Source(), err)
		}
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
