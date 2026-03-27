// Package export provides configuration exporters.
package export

import "encoding/json"

// JSONExporter exports configuration as JSON.
type JSONExporter struct{}

// Export serializes the configuration data as indented JSON.
func (e *JSONExporter) Export(data map[string]any) ([]byte, error) {
	return json.MarshalIndent(data, "", "  ")
}

// Format returns "json".
func (e *JSONExporter) Format() string { return "json" }
