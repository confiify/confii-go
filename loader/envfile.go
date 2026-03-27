package loader

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/internal/dictutil"
	"github.com/qualitycoe/confii-go/internal/typecoerce"
)

// EnvFileLoader loads configuration from a .env file.
// Format: KEY=VALUE per line with support for comments, quoting, and nested keys.
type EnvFileLoader struct {
	source string
}

// NewEnvFile creates a new .env file loader. Defaults to ".env" if path is empty.
func NewEnvFile(path string) *EnvFileLoader {
	if path == "" {
		path = ".env"
	}
	return &EnvFileLoader{source: path}
}

func (l *EnvFileLoader) Source() string { return l.source }

func (l *EnvFileLoader) Load(_ context.Context) (map[string]any, error) {
	f, err := os.Open(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, confii.NewLoadError(l.source, err)
	}
	defer f.Close()

	result := make(map[string]any)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Must contain '='.
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Handle quoting.
		value = unquoteEnvValue(value)

		// Type coerce.
		parsed := typecoerce.ParseScalar(value, false)

		// Support nested keys via dot notation.
		if strings.Contains(key, ".") {
			_ = dictutil.SetNested(result, key, parsed)
		} else {
			result[key] = parsed
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, confii.NewLoadError(l.source, err)
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// unquoteEnvValue handles single-quoted, double-quoted, and unquoted values.
func unquoteEnvValue(value string) string {
	if len(value) >= 2 {
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			// Single-quoted: literal, no escapes.
			return value[1 : len(value)-1]
		}
		if value[0] == '"' && value[len(value)-1] == '"' {
			// Double-quoted: process escape sequences.
			inner := value[1 : len(value)-1]
			inner = strings.ReplaceAll(inner, `\n`, "\n")
			inner = strings.ReplaceAll(inner, `\t`, "\t")
			return inner
		}
	}

	// Unquoted: strip inline comments (" #" and everything after).
	if idx := strings.Index(value, " #"); idx != -1 {
		value = strings.TrimSpace(value[:idx])
	}
	return value
}
