// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"strings"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

const loaderSpecHelp = "Loader spec TYPE:SOURCE (yaml, json, toml, ini, dotenv, environment, or http)"

// parseLoaders parses "type:source" loader specs into Loader instances.
func parseLoaders(specs []string) ([]confii.Loader, error) {
	var loaders []confii.Loader
	for _, spec := range specs {
		typ, source, ok := strings.Cut(spec, ":")
		if !ok {
			return nil, fmt.Errorf("invalid loader spec %q (expected type:source)", spec)
		}
		l, err := createLoader(typ, source)
		if err != nil {
			return nil, err
		}
		loaders = append(loaders, l)
	}
	return loaders, nil
}

func createLoader(typ, source string) (confii.Loader, error) {
	switch strings.ToLower(typ) {
	case "yaml":
		return loader.NewYAML(source), nil
	case "json":
		return loader.NewJSON(source), nil
	case "toml":
		return loader.NewTOML(source), nil
	case "ini":
		return loader.NewINI(source), nil
	case "dotenv":
		return loader.NewEnvFile(source), nil
	case "environment":
		return loader.NewEnvironment(source), nil
	case "http", "https":
		return loader.NewHTTP(source), nil
	default:
		return nil, fmt.Errorf("unknown loader type: %s", typ)
	}
}

// buildConfig creates a Config from CLI flags.
//
// When the caller supplies no -l/--loader specs, buildConfig omits
// [confii.WithLoaders] rather than passing an empty slice. This lets the
// project's declarative `sources` participate through the documented priority
// model: explicit code > self-config > built-in defaults.
func buildConfig(env string, loaderSpecs []string) (*confii.Config[any], error) {
	return buildConfigWithOptions(env, loaderSpecs)
}

func buildConfigWithOptions(env string, loaderSpecs []string, extraOptions ...confii.Option) (*confii.Config[any], error) {
	return buildConfigWithContext(context.Background(), env, loaderSpecs, extraOptions...)
}

func buildConfigWithContext(ctx context.Context, env string, loaderSpecs []string, extraOptions ...confii.Option) (*confii.Config[any], error) {
	loaders, err := parseLoaders(loaderSpecs)
	if err != nil {
		return nil, err
	}

	var opts []confii.Option
	if len(loaders) > 0 {
		opts = append(opts, confii.WithLoaders(loaders...))
	}
	if env != "" {
		opts = append(opts, confii.WithEnv(env))
	}
	opts = append(opts, extraOptions...)

	return confii.NewWithContext[any](ctx, opts...)
}
