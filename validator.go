// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

// Validator checks a materialized configuration map. Implementations must not
// mutate data and return nil only when every configured constraint succeeds.
type Validator interface {
	// Validate returns a descriptive error for one or more violations.
	Validate(data map[string]any) error
}
