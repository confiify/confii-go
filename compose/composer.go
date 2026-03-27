// Package compose processes _include and _defaults directives in configuration
// files, supporting Hydra-style configuration composition with cycle detection.
package compose

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/confiify/confii-go/internal/dictutil"
	"gopkg.in/yaml.v3"
)

const maxDepth = 10

// Composer processes _include and _defaults directives in loaded configurations.
type Composer struct {
	basePath string
	visited  map[string]bool
}

// New creates a new Composer. basePath is used to resolve relative include paths.
func New(basePath string) *Composer {
	if basePath == "" {
		basePath = "."
	}
	return &Composer{
		basePath: basePath,
		visited:  make(map[string]bool),
	}
}

// Compose processes composition directives in the config.
// source is the file path of the config (used for relative path resolution).
// Returns the composed config with _include, _defaults, and _merge_strategy removed.
func (c *Composer) Compose(config map[string]any, source string) (map[string]any, error) {
	return c.compose(config, source, 0)
}

func (c *Composer) compose(config map[string]any, source string, depth int) (map[string]any, error) {
	if depth >= maxDepth {
		return nil, fmt.Errorf("composition max depth (%d) exceeded at %s", maxDepth, source)
	}

	result := make(map[string]any)
	for k, v := range config {
		result[k] = v
	}

	// Step 1: Process _defaults (provide base values).
	if defaults, ok := result["_defaults"]; ok {
		base, err := c.processDefaults(defaults, source, depth)
		if err != nil {
			return nil, err
		}
		// Defaults go underneath current config.
		result = dictutil.DeepMerge(base, result)
		delete(result, "_defaults")
	}

	// Step 2: Process _include (merge additional configs on top).
	if includes, ok := result["_include"]; ok {
		included, err := c.processIncludes(includes, source, depth)
		if err != nil {
			return nil, err
		}
		result = dictutil.DeepMerge(result, included)
		delete(result, "_include")
	}

	// Step 3: Remove _merge_strategy key.
	delete(result, "_merge_strategy")

	return result, nil
}

func (c *Composer) processDefaults(defaults any, source string, depth int) (map[string]any, error) {
	result := make(map[string]any)

	var items []any
	switch v := defaults.(type) {
	case []any:
		items = v
	case string:
		items = []any{v}
	default:
		return result, nil
	}

	for _, item := range items {
		switch v := item.(type) {
		case string:
			// "key: value" format.
			key, val, ok := strings.Cut(v, ":")
			if ok {
				result[strings.TrimSpace(key)] = strings.TrimSpace(val)
			}
		case map[string]any:
			for k, val := range v {
				if k == "optional" {
					continue // metadata, not a default value
				}
				result[k] = val
			}
		}
	}
	return result, nil
}

func (c *Composer) processIncludes(includes any, source string, depth int) (map[string]any, error) {
	result := make(map[string]any)

	var paths []string
	switch v := includes.(type) {
	case string:
		paths = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				paths = append(paths, s)
			}
		}
	default:
		return result, nil
	}

	baseDir := filepath.Dir(source)
	if baseDir == "" || baseDir == "." {
		baseDir = c.basePath
	}

	for _, p := range paths {
		// Resolve path relative to source file's directory.
		resolved := p
		if !filepath.IsAbs(p) {
			resolved = filepath.Join(baseDir, p)
		}

		abs, err := filepath.Abs(resolved)
		if err != nil {
			abs = resolved
		}

		// Cycle detection.
		if c.visited[abs] {
			continue // skip circular include
		}
		c.visited[abs] = true

		included, err := c.loadFile(resolved, depth)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", p, err)
		}
		if included != nil {
			result = dictutil.DeepMerge(result, included)
		}
	}

	return result, nil
}

func (c *Composer) loadFile(path string, depth int) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &result)
	case ".json":
		err = json.Unmarshal(data, &result)
	case ".toml":
		err = toml.Unmarshal(data, &result)
	default:
		err = yaml.Unmarshal(data, &result) // default to YAML
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Recursively compose the included file.
	return c.compose(result, path, depth+1)
}
