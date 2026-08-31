// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package configdecode

import (
	"bytes"

	"github.com/confiify/confii-go/v2/internal/formatparse"
	"go.yaml.in/yaml/v3"
)

// MapWithPositions decodes data exactly as [Map] does and additionally reports
// the one-based source line of each key, addressed by its dotted path.
//
// Only formats whose parser preserves positions can report them. YAML does;
// JSON, TOML, and INI decode through libraries that expose no per-key
// position, so they return a nil map rather than fabricated zeroes. Callers
// must treat a missing entry as "unknown", never as line zero.
func MapWithPositions(data []byte, format formatparse.Format) (map[string]any, map[string]int, error) {
	result, err := Map(data, format)
	if err != nil {
		return nil, nil, err
	}
	if format != formatparse.FormatYAML {
		return result, nil, nil
	}

	// Positions come from a second parse into the node tree. Reusing Map for
	// the values keeps key normalization and every validation rule identical
	// between the two entry points.
	var root yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&root); err != nil {
		// The document already decoded cleanly above, so a node-level failure
		// costs positions only and must not fail the load.
		return result, nil, nil
	}
	positions := make(map[string]int)
	collectYAMLPositions("", &root, positions)
	return result, positions, nil
}

// collectYAMLPositions walks mapping nodes, recording the line of each key.
// Sequence elements are not addressable as configuration keys, so a list is
// recorded at the line of the key that holds it.
func collectYAMLPositions(prefix string, node *yaml.Node, out map[string]int) {
	if node == nil {
		return
	}
	if node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			collectYAMLPositions(prefix, child, out)
		}
		return
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		path := key.Value
		if prefix != "" {
			path = prefix + "." + key.Value
		}
		out[path] = key.Line
		collectYAMLPositions(path, value, out)
	}
}
