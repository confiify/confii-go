// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

// Validator validates a configuration map against a schema.
type Validator interface {
	Validate(data map[string]any) error
}
