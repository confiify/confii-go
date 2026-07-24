// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import "context"

// Loader loads configuration from a source.
// Implementations should return (nil, nil) when the source does not exist
// (graceful absence) and (nil, error) on actual failures.
//
// Both pointer and value receiver implementations are supported; the
// framework introspects the loader's type via reflect.Indirect so a
// non-pointer Loader will not trigger a reflection panic during source
// tracking, layer enumeration, or runtime extension.
type Loader interface {
	// Load reads configuration and returns it as a map.
	Load(ctx context.Context) (map[string]any, error)

	// Source returns a human-readable identifier for this loader
	// (e.g., file path, URL, "environment:APP").
	Source() string
}
