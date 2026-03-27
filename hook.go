package confii

import "github.com/confiify/confii-go/hook"

type (
	// Hook is a function that transforms a configuration value during access.
	// Hooks are executed in registration order and may modify, enrich, or
	// replace the value before it is returned to the caller.
	Hook = hook.Func

	// HookCondition is a predicate that determines whether a conditional
	// hook should fire for a given key and value. Returning true causes the
	// associated [Hook] to execute; returning false skips it.
	HookCondition = hook.Condition
)
