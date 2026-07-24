package confii

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvironmentFilesSourceValidationBranches(t *testing.T) {
	tests := []struct {
		name    string
		source  map[string]any
		message string
	}{
		{name: "search paths scalar", source: map[string]any{"search_paths": "config"}, message: "non-empty list"},
		{name: "search paths empty", source: map[string]any{"search_paths": []any{}}, message: "non-empty list"},
		{name: "search paths contains non-string", source: map[string]any{"search_paths": []any{"config", 42}}, message: "contains int"},
		{name: "search paths contains empty", source: map[string]any{"search_paths": []any{"config", " "}}, message: "contains an empty string"},
		{name: "default file wrong type", source: map[string]any{"default_file": true}, message: "default_file"},
		{name: "default file empty", source: map[string]any{"default_file": " "}, message: "default_file"},
		{name: "environment file wrong type", source: map[string]any{"environment_file": true}, message: "environment_file"},
		{name: "environment file empty", source: map[string]any{"environment_file": " "}, message: "environment_file"},
		{name: "default file is path", source: map[string]any{"default_file": "nested/default.yaml"}, message: "file name, not a path"},
		{name: "environment file is path", source: map[string]any{"environment_file": "nested/{environment}.yaml"}, message: "file name, not a path"},
		{name: "default required wrong type", source: map[string]any{"default_required": "true"}, message: "must be a boolean"},
		{name: "environment required wrong type", source: map[string]any{"environment_required": "true"}, message: "must be a boolean"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseEnvironmentFilesSource(tt.source)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrConfigLoad)
			assert.Contains(t, err.Error(), tt.message)
		})
	}

	cfg, err := parseEnvironmentFilesSource(map[string]any{
		"search_paths":         []string{" settings ", "config"},
		"default_required":     true,
		"environment_required": false,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"settings", "config"}, cfg.searchPaths)
	assert.True(t, cfg.defaultRequired)
	assert.False(t, cfg.environmentRequired)
}

func TestEnvironmentFileValidationAndDiscoveryBranches(t *testing.T) {
	for _, env := range []string{".", "..", "prod..backup", "-production", "prod/blue", "prod blue"} {
		t.Run("invalid env "+env, func(t *testing.T) {
			err := validateEnvironmentName(env)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrConfigLoad)
		})
	}
	require.NoError(t, validateEnvironmentName("prod.eu-west_1"))

	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "config"), 0o750))
	path, candidates, err := findEnvironmentFile(root, []string{"config", "."}, "missing.yaml")
	require.NoError(t, err)
	assert.Empty(t, path)
	assert.Len(t, candidates, 2)

	require.NoError(t, os.Mkdir(filepath.Join(root, "config", "not-a-file.yaml"), 0o750))
	_, _, err = findEnvironmentFile(root, []string{"config"}, "not-a-file.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a regular file")

	absDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(absDir, "default.yaml"), []byte("value: absolute\n"), 0o600))
	found, _, err := findEnvironmentFile(root, []string{absDir}, "default.yaml")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(absDir, "default.yaml"), found)

	err = environmentFilesError("inspect", errors.New("permission denied"))
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestBuildEnvironmentFileLoadersOptionalEnvironment(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "config"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config", "default.yaml"), []byte("value: base\n"), 0o600))

	opts := defaultOptions()
	opts.WorkingDir = root
	opts.Env = "production"
	loaders, err := buildEnvironmentFileLoaders(&opts, map[string]any{
		"environment_required": false,
	})
	require.NoError(t, err)
	require.Len(t, loaders, 1)
	selected, ok := loaders[0].(interface{ selectedEnvironmentFile() bool })
	require.True(t, ok)
	assert.True(t, selected.selectedEnvironmentFile())
}

func TestBuildEnvironmentFileLoadersDiscoveryErrorBranches(t *testing.T) {
	t.Run("project root resolution error", func(t *testing.T) {
		opts := defaultOptions()
		_, err := buildEnvironmentFileLoadersWithAbs(&opts, map[string]any{}, func(string) (string, error) {
			return "", errors.New("working directory unavailable")
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrConfigLoad)
		assert.Contains(t, err.Error(), "resolve project root: working directory unavailable")
	})

	t.Run("empty working directory uses current directory", func(t *testing.T) {
		opts := defaultOptions()
		loaders, err := buildEnvironmentFileLoaders(&opts, map[string]any{
			"search_paths":         []any{t.TempDir()},
			"environment_required": false,
		})
		require.NoError(t, err)
		assert.Empty(t, loaders)
	})

	t.Run("default candidate inspection error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "blocked"), []byte("file"), 0o600))
		opts := defaultOptions()
		opts.WorkingDir = root
		_, err := buildEnvironmentFileLoaders(&opts, map[string]any{
			"search_paths": []any{"blocked"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inspect candidate")
	})

	t.Run("environment candidate inspection error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "config"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "config", "default.yaml"), []byte("value: base\n"), 0o600))
		require.NoError(t, os.Mkdir(filepath.Join(root, "config", "production.yaml"), 0o750))
		opts := defaultOptions()
		opts.WorkingDir = root
		opts.Env = "production"
		_, err := buildEnvironmentFileLoaders(&opts, map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a regular file")
	})
}
