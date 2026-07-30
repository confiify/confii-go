// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

// Exporter serializes a materialized configuration map. Implementations must
// not mutate data and should return an error when a value cannot be represented
// by the target format. Exporters do not redact secrets; callers are
// responsible for protecting serialized output.
type Exporter interface {
	// Export serializes data and returns a newly owned byte slice.
	Export(data map[string]any) ([]byte, error)
	// Format returns the stable lowercase format name, such as "json", "yaml",
	// or "toml".
	Format() string
}
