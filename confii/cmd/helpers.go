package cmd

import (
	"context"
	"fmt"
	"strings"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
)

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
	case "env_file", "envfile":
		return loader.NewEnvFile(source), nil
	case "env", "environment":
		return loader.NewEnvironment(source), nil
	case "http", "https":
		return loader.NewHTTP(source), nil
	default:
		return nil, fmt.Errorf("unknown loader type: %s", typ)
	}
}

// buildConfig creates a Config from CLI flags.
func buildConfig(env string, loaderSpecs []string) (*confii.Config[any], error) {
	loaders, err := parseLoaders(loaderSpecs)
	if err != nil {
		return nil, err
	}

	opts := []confii.Option{confii.WithLoaders(loaders...)}
	if env != "" {
		opts = append(opts, confii.WithEnv(env))
	}

	return confii.New[any](context.Background(), opts...)
}
