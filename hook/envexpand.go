// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"
	"os"
	"regexp"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)\}`)
var namedEnvVarPattern = regexp.MustCompile(`\$\{env:([^}:]+)\}`)

// NewEnvExpanderHook returns a hook that replaces ${VAR} and ${env:VAR}
// placeholders with values from os.Environ. Unknown variables are left
// unchanged.
func NewEnvExpanderHook() Func {
	return func(_ context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok {
			return value, nil
		}
		s = namedEnvVarPattern.ReplaceAllStringFunc(s, func(match string) string {
			groups := namedEnvVarPattern.FindStringSubmatch(match)
			if v, exists := os.LookupEnv(groups[1]); exists {
				return v
			}
			return match
		})
		return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
			groups := envVarPattern.FindStringSubmatch(match)
			if v, exists := os.LookupEnv(groups[1]); exists {
				return v
			}
			return match
		}), nil
	}
}
