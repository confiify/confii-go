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

// Merger combines a base configuration map with an overlay map. The composer
// uses it to fold included files and defaults onto the current configuration.
//
// The interface is intentionally narrow so callers may pass any
// strategy-aware merger (for example, the project's merge.DefaultMerger or
// merge.AdvancedMerger) without the compose package taking a hard import of
// the merge package.
type Merger interface {
	Merge(base, overlay map[string]any) map[string]any
}

// deepMerger is the default Merger used when none is configured. It delegates
// to dictutil.DeepMerge to preserve historical composition semantics.
type deepMerger struct{}

func (deepMerger) Merge(base, overlay map[string]any) map[string]any {
	return dictutil.DeepMerge(base, overlay)
}

// Composer processes _include and _defaults directives in loaded
// configurations.
//
// A Composer is safe to reuse across multiple [Composer.Compose] calls: the
// cycle-detection "visited" set lives on the call stack, not on the Composer
// itself, so each top-level invocation starts with a fresh seen-set. This
// means legitimate re-includes from a subsequent call (or after a config
// reload) are processed normally instead of being silently skipped, while the
// cycle-detection guarantee within any single traversal remains intact.
type Composer struct {
	basePath string
	// merger combines included/default maps into the result. It defaults to
	// dictutil.DeepMerge via deepMerger when nil; callers may inject a
	// strategy-aware merger via [Composer.WithMerger] or [New] options.
	merger Merger
}

// Option configures a Composer at construction time.
type Option func(*Composer)

// WithMerger configures the merger used to fold included files and defaults
// into the current configuration. Passing nil keeps the deep-merge default.
func WithMerger(m Merger) Option {
	return func(c *Composer) {
		if m != nil {
			c.merger = m
		}
	}
}

// New creates a new Composer. basePath is used to resolve relative include
// paths. Optional [Option] values configure merger and other behaviors.
func New(basePath string, opts ...Option) *Composer {
	if basePath == "" {
		basePath = "."
	}
	c := &Composer{
		basePath: basePath,
		merger:   deepMerger{},
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.merger == nil {
		c.merger = deepMerger{}
	}
	return c
}

// SetMerger overrides the merger used by the composer after construction.
// Passing nil resets it to the deep-merge default.
func (c *Composer) SetMerger(m Merger) {
	if m == nil {
		c.merger = deepMerger{}
		return
	}
	c.merger = m
}

// Compose processes _include, _defaults, and _merge_strategy directives in
// config, resolving relative paths against source. Returns the fully composed
// configuration with all directives removed.
//
// The cycle-detection state is local to this call: invoking Compose again on
// the same Composer (for example, during a reload) starts with an empty
// seen-set so legitimate re-includes are honored.
func (c *Composer) Compose(config map[string]any, source string) (map[string]any, error) {
	result, _, err := c.ComposeWithDependencies(config, source)
	return result, err
}

// ComposeWithDependencies behaves like [Composer.Compose] and additionally
// returns every file read through _include, including transitive includes.
// Paths are absolute and ordered by first traversal. Callers use this data to
// watch and fingerprint the complete composition input set.
func (c *Composer) ComposeWithDependencies(config map[string]any, source string) (map[string]any, []string, error) {
	visited := make(map[string]bool)
	var dependencies []string
	result, err := c.compose(config, source, 0, visited, &dependencies)
	return result, dependencies, err
}

func (c *Composer) compose(config map[string]any, source string, depth int, visited map[string]bool, dependencies *[]string) (map[string]any, error) {
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
		result = c.merger.Merge(base, result)
		delete(result, "_defaults")
	}

	// Step 2: Process _include (merge additional configs on top).
	if includes, ok := result["_include"]; ok {
		included, err := c.processIncludes(includes, source, depth, visited, dependencies)
		if err != nil {
			return nil, err
		}
		result = c.merger.Merge(result, included)
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

func (c *Composer) processIncludes(includes any, source string, depth int, visited map[string]bool, dependencies *[]string) (map[string]any, error) {
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

		// Cycle detection within this single traversal.
		if visited[abs] {
			continue // skip circular include
		}
		visited[abs] = true
		*dependencies = append(*dependencies, abs)

		included, err := c.loadFile(resolved, depth, visited, dependencies)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", p, err)
		}
		if included != nil {
			result = c.merger.Merge(result, included)
		}
	}

	return result, nil
}

func (c *Composer) loadFile(path string, depth int, visited map[string]bool, dependencies *[]string) (map[string]any, error) {
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

	// Recursively compose the included file, threading visited through so
	// cycle detection remains intact within this single top-level call.
	return c.compose(result, path, depth+1, visited, dependencies)
}
