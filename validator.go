// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

// Validator checks a materialized configuration map. Register application
// rules with [WithValidator]. Confii invokes them during every enabled
// snapshot-validation transaction and supplies an independent copy of the
// candidate. Implementations should still treat the input as read-only and
// return nil only when every configured constraint succeeds.
type Validator interface {
	// Validate returns a descriptive error for one or more violations.
	Validate(data map[string]any) error
}
