package loader

import (
	"context"
	"errors"
	"os"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/internal/typecoerce"
	"gopkg.in/ini.v1"
)

// INILoader loads configuration from an INI file.
// Each section becomes a top-level key; keys within sections are nested.
type INILoader struct {
	source string
}

// NewINI creates a new INI loader for the given file path.
func NewINI(path string) *INILoader {
	return &INILoader{source: path}
}

// Source returns the identifier for this loader's configuration source.
func (l *INILoader) Source() string { return l.source }

// Load reads and parses the INI file at the configured path, returning sections as nested configuration maps.
func (l *INILoader) Load(_ context.Context) (map[string]any, error) {
	if _, err := os.Stat(l.source); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, confii.NewLoadError(l.source, err)
	}

	cfg, err := ini.Load(l.source)
	if err != nil {
		return nil, confii.NewFormatError(l.source, "ini", err)
	}

	result := make(map[string]any)
	for _, section := range cfg.Sections() {
		name := section.Name()
		if name == "DEFAULT" {
			continue
		}
		sectionMap := make(map[string]any)
		for _, key := range section.Keys() {
			sectionMap[key.Name()] = typecoerce.ParseScalar(key.Value(), false)
		}
		result[name] = sectionMap
	}

	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
