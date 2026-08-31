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

// PositionalLoader is an optional interface a [Loader] may implement to report
// where in its source each key was defined. Confii type-asserts for it after
// every load and records the reported lines as SourceInfo.LineNumber, so
// introspection can point at the exact line a value came from.
//
// Implementing it is opt-in and never required: a Loader whose format or
// transport carries no position information simply does not implement it, and
// its keys report line zero. Among the bundled loaders only YAML can supply
// positions, because JSON, TOML, and INI decode through libraries that expose
// no per-key location.
type PositionalLoader interface {
	Loader

	// Positions returns the one-based source line of each key produced by the
	// most recent Load, addressed by dotted key path. Keys the loader cannot
	// locate are absent rather than zero. The returned map is owned by the
	// caller and must not alias loader state.
	Positions() map[string]int
}

// loaderPositions reports the key positions a loader can supply, or nil when it
// does not implement [PositionalLoader].
func loaderPositions(l Loader) map[string]int {
	positional, ok := l.(PositionalLoader)
	if !ok {
		return nil
	}
	return positional.Positions()
}
