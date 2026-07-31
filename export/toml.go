// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package export

import (
	"strings"

	"github.com/BurntSushi/toml"
)

// TOMLExporter exports configuration as TOML.
type TOMLExporter struct{}

// Export serializes the configuration data as TOML.
func (e *TOMLExporter) Export(data map[string]any) ([]byte, error) {
	var output strings.Builder
	enc := toml.NewEncoder(&output)
	if err := enc.Encode(data); err != nil {
		return nil, err
	}
	return []byte(output.String()), nil
}

// Format returns "toml".
func (e *TOMLExporter) Format() string { return "toml" }
