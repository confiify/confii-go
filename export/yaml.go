package export

import "gopkg.in/yaml.v3"

// YAMLExporter exports configuration as YAML.
type YAMLExporter struct{}

// Export serializes the configuration data as YAML.
func (e *YAMLExporter) Export(data map[string]any) ([]byte, error) {
	return yaml.Marshal(data)
}

// Format returns "yaml".
func (e *YAMLExporter) Format() string { return "yaml" }
