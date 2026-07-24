package confii

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const environmentPlaceholder = "{environment}"

type environmentFilesSource struct {
	searchPaths         []string
	defaultFile         string
	environmentFile     string
	defaultRequired     bool
	environmentRequired bool
}

func buildEnvironmentFileLoaders(opts *options, src map[string]any) ([]Loader, error) {
	return buildEnvironmentFileLoadersWithAbs(opts, src, filepath.Abs)
}

func buildEnvironmentFileLoadersWithAbs(
	opts *options,
	src map[string]any,
	absPath func(string) (string, error),
) ([]Loader, error) {
	cfg, err := parseEnvironmentFilesSource(src)
	if err != nil {
		return nil, err
	}

	root := opts.WorkingDir
	if root == "" {
		root = "."
	}
	root, err = absPath(root)
	if err != nil {
		return nil, environmentFilesError("resolve project root", err)
	}

	var loaders []Loader
	defaultPath, candidates, err := findEnvironmentFile(root, cfg.searchPaths, cfg.defaultFile)
	if err != nil {
		return nil, err
	}
	if defaultPath != "" {
		loaders = append(loaders, newEnvironmentFileLoader(defaultPath, "default", opts))
	} else if cfg.defaultRequired {
		return nil, missingEnvironmentFileError("default", candidates)
	}

	env := opts.Env
	if env == "" {
		return loaders, nil
	}
	if err := validateEnvironmentName(env); err != nil {
		return nil, err
	}

	environmentName := strings.ReplaceAll(cfg.environmentFile, environmentPlaceholder, env)
	environmentPath, candidates, err := findEnvironmentFile(root, cfg.searchPaths, environmentName)
	if err != nil {
		return nil, err
	}
	if environmentPath != "" {
		loaders = append(loaders, newEnvironmentFileLoader(environmentPath, "environment", opts))
	} else if cfg.environmentRequired {
		return nil, missingEnvironmentFileError(fmt.Sprintf("environment %q", env), candidates)
	}

	return loaders, nil
}

// environmentFileLoader marks a file whose environment role was already
// selected during discovery. config_load therefore treats its contribution as
// flat data instead of passing it through the section-based resolver again.
type environmentFileLoader struct {
	file *fileAutoLoader
	role string
}

func newEnvironmentFileLoader(path, role string, opts *options) Loader {
	return &environmentFileLoader{
		file: &fileAutoLoader{path: path, errorPolicy: opts.OnError, logger: opts.Logger},
		role: role,
	}
}

func (l *environmentFileLoader) Source() string { return l.file.Source() }

func (l *environmentFileLoader) Load(ctx context.Context) (map[string]any, error) {
	return l.file.Load(ctx)
}

func (l *environmentFileLoader) selectedEnvironmentFile() bool { return true }

func (l *environmentFileLoader) environmentFileRole() string { return l.role }

func parseEnvironmentFilesSource(src map[string]any) (environmentFilesSource, error) {
	cfg := environmentFilesSource{
		searchPaths:         []string{"config", "."},
		defaultFile:         "default.yaml",
		environmentFile:     environmentPlaceholder + ".yaml",
		defaultRequired:     false,
		environmentRequired: true,
	}

	if raw, ok := src["search_paths"]; ok {
		paths, err := stringList(raw)
		if err != nil || len(paths) == 0 {
			return cfg, environmentFilesError("`search_paths` must be a non-empty list of non-empty strings", err)
		}
		cfg.searchPaths = paths
	}
	if raw, ok := src["default_file"]; ok {
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return cfg, environmentFilesError("`default_file` must be a non-empty string", nil)
		}
		cfg.defaultFile = strings.TrimSpace(value)
	}
	if raw, ok := src["environment_file"]; ok {
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return cfg, environmentFilesError("`environment_file` must be a non-empty string", nil)
		}
		cfg.environmentFile = strings.TrimSpace(value)
	}
	if !strings.Contains(cfg.environmentFile, environmentPlaceholder) {
		return cfg, environmentFilesError("`environment_file` must contain the {environment} placeholder", nil)
	}
	if err := validateEnvironmentFilename("default_file", cfg.defaultFile); err != nil {
		return cfg, err
	}
	if err := validateEnvironmentFilename("environment_file", cfg.environmentFile); err != nil {
		return cfg, err
	}

	var err error
	if cfg.defaultRequired, err = optionalBool(src, "default_required", cfg.defaultRequired); err != nil {
		return cfg, err
	}
	if cfg.environmentRequired, err = optionalBool(src, "environment_required", cfg.environmentRequired); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func stringList(raw any) ([]string, error) {
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []string:
		result := make([]string, 0, len(typed))
		for _, value := range typed {
			result = append(result, strings.TrimSpace(value))
		}
		return nonEmptyStrings(result)
	default:
		return nil, fmt.Errorf("got %T", raw)
	}

	result := make([]string, 0, len(values))
	for _, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("contains %T", rawValue)
		}
		result = append(result, strings.TrimSpace(value))
	}
	return nonEmptyStrings(result)
}

func nonEmptyStrings(values []string) ([]string, error) {
	for _, value := range values {
		if value == "" {
			return nil, errors.New("contains an empty string")
		}
	}
	return values, nil
}

func optionalBool(src map[string]any, key string, fallback bool) (bool, error) {
	raw, ok := src[key]
	if !ok {
		return fallback, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, environmentFilesError(fmt.Sprintf("`%s` must be a boolean", key), nil)
	}
	return value, nil
}

func validateEnvironmentFilename(field, value string) error {
	if filepath.IsAbs(value) || filepath.Base(value) != value || value == "." || value == ".." {
		return environmentFilesError(fmt.Sprintf("`%s` must be a file name, not a path", field), nil)
	}
	return nil
}

func validateEnvironmentName(env string) error {
	if env == "." || env == ".." || strings.Contains(env, "..") {
		return environmentFilesError(fmt.Sprintf("unsafe environment name %q", env), nil)
	}
	for i, r := range env {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || (i > 0 && (r == '-' || r == '_' || r == '.'))
		if !valid {
			return environmentFilesError(fmt.Sprintf("unsafe environment name %q: use letters, digits, '.', '_' or '-'", env), nil)
		}
	}
	return nil
}

func findEnvironmentFile(root string, searchPaths []string, name string) (string, []string, error) {
	candidates := make([]string, 0, len(searchPaths))
	for _, dir := range searchPaths {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		candidate := filepath.Clean(filepath.Join(dir, name))
		candidates = append(candidates, candidate)
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.Mode().IsRegular():
			return candidate, candidates, nil
		case err == nil:
			return "", candidates, environmentFilesError(fmt.Sprintf("candidate %q is not a regular file", candidate), nil)
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return "", candidates, environmentFilesError(fmt.Sprintf("inspect candidate %q", candidate), err)
		}
	}
	return "", candidates, nil
}

func missingEnvironmentFileError(role string, candidates []string) error {
	return environmentFilesError(fmt.Sprintf("required %s file was not found; searched: %s", role, strings.Join(candidates, ", ")), nil)
}

func environmentFilesError(message string, cause error) error {
	if cause != nil {
		message = message + ": " + cause.Error()
	}
	return &ConfigError{Op: "ApplySelfConfig", Err: fmt.Errorf("%w: environment_files: %s", ErrConfigLoad, message)}
}
