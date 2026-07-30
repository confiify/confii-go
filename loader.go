// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import "context"

// Loader supplies one configuration layer. Confii invokes Load with the
// initialization, reload, or extension context and merges the returned map at
// the loader's configured position.
//
// Implementations must honor context cancellation for blocking work. Return
// (nil, nil) only when absence is an intentional, non-error result; return a
// non-nil error for malformed data, authorization failures, transport errors,
// or other conditions that should be handled by [WithOnError]. Source must be
// stable and must not contain credentials or secret values because it appears
// in diagnostics and source-tracking output.
type Loader interface {
	// Load reads and parses the source into a string-keyed configuration map.
	// The returned map becomes owned by Confii and may be copied or normalized.
	Load(ctx context.Context) (map[string]any, error)

	// Source returns a non-sensitive identifier such as a file path, URL, or
	// "environment:APP" marker.
	Source() string
}
