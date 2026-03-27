package hook

import (
	"os"
	"regexp"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)\}`)

// NewEnvExpanderHook returns a hook that replaces ${VAR} placeholders
// with values from os.Environ. Unknown variables are left unchanged.
func NewEnvExpanderHook() Func {
	return func(_ string, value any) any {
		s, ok := value.(string)
		if !ok {
			return value
		}
		return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
			groups := envVarPattern.FindStringSubmatch(match)
			if len(groups) < 2 {
				return match
			}
			if v, exists := os.LookupEnv(groups[1]); exists {
				return v
			}
			return match
		})
	}
}
