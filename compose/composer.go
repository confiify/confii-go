// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package compose processes _include and _defaults directives in configuration
// files, supporting Hydra-style configuration composition with cycle detection.
package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/confiify/confii-go/v2/internal/configdecode"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/internal/formatparse"
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

// deepMerger is the default Merger and recursively merges maps.
type deepMerger struct{}

func (deepMerger) Merge(base, overlay map[string]any) map[string]any {
	return dictutil.DeepMerge(base, overlay)
}

// Composer processes _include and _defaults directives in loaded
// configurations.
//
// A Composer may be reused and Compose operations may run concurrently when
// its Merger is concurrency-safe. Do not call [Composer.SetMerger]
// concurrently with composition. Each call detects cycles independently, so
// includes remain eligible on later reloads.
type Composer struct {
	basePath     string
	absolutePath func(string) (string, error)
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
		basePath:     basePath,
		absolutePath: filepath.Abs,
		merger:       deepMerger{},
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
// config, resolving relative paths against source and the Composer base path.
// It returns a new top-level map with composition directives removed. Includes
// may be YAML, JSON, or TOML; format is selected from the file extension and a
// declared format is rejected when content is incompatible. Missing or
// malformed includes, include cycles beyond the maximum depth, and parsing
// failures return an error without mutating config.
func (c *Composer) Compose(config map[string]any, source string) (map[string]any, error) {
	return c.ComposeWithContext(context.Background(), config, source)

}

// ComposeWithContext is the context-aware form of [Composer.Compose]. A nil or
// canceled context returns an error. Local file reads cannot be interrupted
// once started, but cancellation is checked around each read and parse step.
func (c *Composer) ComposeWithContext(ctx context.Context, config map[string]any, source string) (map[string]any, error) {
	result, _, err := c.ComposeWithDependenciesWithContext(ctx, config, source)
	return result, err
}

// ComposeWithDependencies behaves like [Composer.Compose] and additionally
// returns every file read through _include, including transitive includes.
// Paths are absolute and ordered by first traversal. Callers use this data to
// watch and fingerprint the complete composition input set.
func (c *Composer) ComposeWithDependencies(config map[string]any, source string) (map[string]any, []string, error) {
	return c.ComposeWithDependenciesWithContext(context.Background(), config, source)
}

// ComposeWithDependenciesWithContext stops recursive include processing when ctx
// is canceled. Individual local file reads are not interruptible, but Confii
// checks cancellation immediately before and after each read and parse step.
func (c *Composer) ComposeWithDependenciesWithContext(ctx context.Context, config map[string]any, source string) (map[string]any, []string, error) {
	if ctx == nil {
		return nil, nil, errors.New("compose: nil context")
	}
	visited := make(map[string]bool)
	var dependencies []string
	result, err := c.compose(ctx, config, source, 0, visited, &dependencies)
	return result, dependencies, err
}

func (c *Composer) compose(ctx context.Context, config map[string]any, source string, depth int, visited map[string]bool, dependencies *[]string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth >= maxDepth {
		return nil, fmt.Errorf("composition max depth (%d) exceeded at %s", maxDepth, source)
	}

	result := make(map[string]any)
	for k, v := range config {
		result[k] = v
	}

	// Apply defaults before includes so included sources can override them.
	if defaults, ok := result["_defaults"]; ok {
		base := c.processDefaults(defaults)
		// Defaults go underneath current config.
		result = c.merger.Merge(base, result)
		delete(result, "_defaults")
	}

	// Merge included sources on top of the defaults and local values.
	if includes, ok := result["_include"]; ok {
		included, err := c.processIncludes(ctx, includes, source, depth, visited, dependencies)
		if err != nil {
			return nil, err
		}
		result = c.merger.Merge(result, included)
		delete(result, "_include")
	}

	// Composition directives are not part of the published configuration.
	delete(result, "_merge_strategy")

	return result, nil
}

func (c *Composer) processDefaults(defaults any) map[string]any {
	result := make(map[string]any)

	var items []any
	switch v := defaults.(type) {
	case []any:
		items = v
	case string:
		items = []any{v}
	default:
		return result
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
	return result
}

func (c *Composer) processIncludes(ctx context.Context, includes any, source string, depth int, visited map[string]bool, dependencies *[]string) (map[string]any, error) {
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Resolve path relative to source file's directory.
		resolved := p
		if !filepath.IsAbs(p) {
			resolved = filepath.Join(baseDir, p)
		}

		canonical, err := c.absolutePath(resolved)
		if err != nil {
			return nil, fmt.Errorf("resolve include %s: %w", p, err)
		}
		canonical = filepath.Clean(canonical)

		// Cycle detection within this single traversal.
		if visited[canonical] {
			continue // skip circular include
		}
		visited[canonical] = true
		*dependencies = append(*dependencies, canonical)

		included, err := c.loadFile(ctx, resolved, depth, visited, dependencies)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", p, err)
		}
		if included != nil {
			result = c.merger.Merge(result, included)
		}
	}

	return result, nil
}

func (c *Composer) loadFile(ctx context.Context, path string, depth int, visited map[string]bool, dependencies *[]string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// #nosec G304 -- explicit and included configuration paths are the composer's documented input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(path))
	format := formatparse.FromExtension(path)
	if format != formatparse.FormatYAML && format != formatparse.FormatJSON && format != formatparse.FormatTOML {
		return nil, fmt.Errorf("parse %s: unsupported configuration extension %q", path, ext)
	}
	result, err := configdecode.Map(data, format)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Recursively compose the included file, threading visited through so
	// cycle detection remains intact within this single top-level call.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.compose(ctx, result, path, depth+1, visited, dependencies)
}
