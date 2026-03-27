// Package export provides configuration exporters.
package export

import "encoding/json"

// JSONExporter exports configuration as JSON.
type JSONExporter struct{}

func (e *JSONExporter) Export(data map[string]any) ([]byte, error) {
	return json.MarshalIndent(data, "", "  ")
}

func (e *JSONExporter) Format() string { return "json" }
