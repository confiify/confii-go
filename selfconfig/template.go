// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package selfconfig

import (
	"bytes"
	_ "embed"
)

// defaultYAML is the authoritative project self-configuration template used
// by the confii init command. Keeping it in the selfconfig package makes the
// generated file travel with the CLI binary and lets tests detect schema drift.
//
//go:embed default.confii.yaml
var defaultYAML []byte

// DefaultYAML returns a copy of Confii's complete, safe-by-default YAML
// self-configuration template. Callers may modify the returned slice without
// changing later results.
func DefaultYAML() []byte {
	return bytes.Clone(defaultYAML)
}
