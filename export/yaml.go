package export

import "gopkg.in/yaml.v3"

// YAMLExporter exports configuration as YAML.
type YAMLExporter struct{}

func (e *YAMLExporter) Export(data map[string]any) ([]byte, error) {
	return yaml.Marshal(data)
}

func (e *YAMLExporter) Format() string { return "yaml" }
