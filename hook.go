// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import "github.com/confiify/confii-go/v2/hook"

type (
	// Hook transforms one leaf value while a candidate snapshot is materialized.
	// It receives the full dot-separated key and the output of the preceding
	// hook. Returning an error rejects the candidate; read methods do not rerun
	// hooks after publication.
	Hook = hook.Func

	// HookCondition is a context-aware predicate that determines whether a
	// conditional hook should run for a key and its current candidate value. An
	// error aborts materialization before the associated hook is invoked.
	HookCondition = hook.Condition
)
