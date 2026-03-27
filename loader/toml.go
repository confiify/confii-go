package loader

import (
	"context"
	"errors"
	"os"

	"github.com/BurntSushi/toml"
	confii "github.com/confiify/confii-go"
)

// TOMLLoader loads configuration from a TOML file.
type TOMLLoader struct {
	source string
}

// NewTOML creates a new TOML loader for the given file path.
func NewTOML(path string) *TOMLLoader {
	return &TOMLLoader{source: path}
}

func (l *TOMLLoader) Source() string { return l.source }

func (l *TOMLLoader) Load(_ context.Context) (map[string]any, error) {
	data, err := os.ReadFile(l.source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, confii.NewLoadError(l.source, err)
	}

	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		return nil, confii.NewFormatError(l.source, "toml", err)
	}
	return result, nil
}
