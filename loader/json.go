package loader

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	confii "github.com/qualitycoe/confii-go"
)

// JSONLoader loads configuration from a JSON file.
type JSONLoader struct {
	source string
}

// NewJSON creates a new JSON loader for the given file path.
func NewJSON(path string) *JSONLoader {
	return &JSONLoader{source: path}
}

func (l *JSONLoader) Source() string { return l.source }

func (l *JSONLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, confii.NewLoadError(l.source, err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, confii.NewFormatError(l.source, "json", err)
	}
	return result, nil
}
