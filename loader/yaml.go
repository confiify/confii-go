// Package loader provides implementations of the confii.Loader interface
// for various configuration sources.
package loader

import (
	"context"
	"errors"
	"os"

	confii "github.com/confiify/confii-go"
	"gopkg.in/yaml.v3"
)

// YAMLLoader loads configuration from a YAML file.
type YAMLLoader struct {
	source string
}

// NewYAML creates a new YAML loader for the given file path.
func NewYAML(path string) *YAMLLoader {
	return &YAMLLoader{source: path}
}

func (l *YAMLLoader) Source() string { return l.source }

func (l *YAMLLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, confii.NewLoadError(l.source, err)
	}

	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, confii.NewFormatError(l.source, "yaml", err)
	}
	return result, nil
}
