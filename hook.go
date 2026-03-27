package confii

import "github.com/qualitycoe/confii-go/hook"

// Re-export hook types for convenience.
type (
	// Hook transforms a configuration value during access.
	Hook = hook.Func
	// HookCondition determines whether a conditional hook should fire.
	HookCondition = hook.Condition
)
