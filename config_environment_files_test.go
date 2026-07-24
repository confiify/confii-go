package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeEnvironmentFilesProject(t *testing.T, selfConfig string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte(selfConfig), 0o600))
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)
	return root
}

func TestEnvironmentFiles_ConfigDirectoryWinsAndEnvironmentOverridesDefault(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
default_environment: production
debug_mode: true
sources:
  - type: environment_files
`, map[string]string{
		"default.yaml":           "server:\n  host: root-default\n  port: 7000\n",
		"production.yaml":        "server:\n  host: root-production\n",
		"config/default.yaml":    "server:\n  host: config-default\n  port: 8080\n",
		"config/production.yaml": "server:\n  host: config-production\n",
	})

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)

	assert.Equal(t, "production", cfg.Env())
	assert.Equal(t, "config-production", cfg.GetStringOr("server.host", ""))
	assert.Equal(t, 8080, cfg.GetIntOr("server.port", 0))

	layers := cfg.Layers()
	require.Len(t, layers, 2)
	assert.Equal(t, filepath.Join(root, "config", "default.yaml"), layers[0]["source"])
	assert.Equal(t, filepath.Join(root, "config", "production.yaml"), layers[1]["source"])
}

func TestEnvironmentFiles_EnvSwitcherSelectsNamedFile(t *testing.T) {
	t.Setenv("CONFII_ENV_FILES_ENV", "staging")
	root := writeEnvironmentFilesProject(t, `
default_environment: development
env_switcher: CONFII_ENV_FILES_ENV
sources:
  - type: environment_files
`, map[string]string{
		"config/default.yaml": "name: default\n",
		"config/staging.yaml": "name: staging\n",
	})

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.Env())
	assert.Equal(t, "staging", cfg.GetStringOr("name", ""))
}

func TestEnvironmentFiles_CustomSearchPathAndTemplate(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
default_environment: qa
sources:
  - type: environment_files
    search_paths: [settings, config]
    default_file: base.yml
    environment_file: app-{environment}.yml
`, map[string]string{
		"settings/base.yml":   "value: base\nshared: settings\n",
		"settings/app-qa.yml": "value: qa\n",
		"config/base.yml":     "shared: config\n",
	})

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	assert.Equal(t, "qa", cfg.GetStringOr("value", ""))
	assert.Equal(t, "settings", cfg.GetStringOr("shared", ""))
}

func TestEnvironmentFiles_TreatsNamedFilesAsFlatConfiguration(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: environment_files
`, map[string]string{
		"config/default.yaml":    "production:\n  endpoint: nested-config-value\nserver:\n  port: 8080\n",
		"config/production.yaml": "server:\n  host: api.example.com\n",
	})

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	assert.Equal(t, "nested-config-value", cfg.GetStringOr("production.endpoint", ""))
	assert.Equal(t, 8080, cfg.GetIntOr("server.port", 0))
	assert.Equal(t, "api.example.com", cfg.GetStringOr("server.host", ""))
}

func TestEnvironmentFiles_ComposesWithLaterFlatSources(t *testing.T) {
	t.Setenv("ENVFILES_SERVER__PORT", "9090")
	root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: environment_files
  - type: environment
    prefix: ENVFILES
`, map[string]string{
		"config/default.yaml":    "server:\n  host: localhost\n  port: 8080\n",
		"config/production.yaml": "server:\n  host: api.example.com\n",
	})

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", cfg.GetStringOr("server.host", ""))
	assert.Equal(t, 9090, cfg.GetIntOr("server.port", 0))
}

func TestEnvironmentFiles_NoSelectedEnvironmentLoadsOnlyOptionalDefault(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
sources:
  - type: environment_files
`, map[string]string{"config/default.yaml": "enabled: true\n"})

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	assert.Empty(t, cfg.Env())
	assert.True(t, cfg.GetBoolOr("enabled", false))
}

func TestEnvironmentFiles_RequiredAndValidationErrors(t *testing.T) {
	t.Run("missing environment file", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: environment_files
`, map[string]string{"config/default.yaml": "value: default\n"})

		_, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.Error(t, err)
		assert.ErrorIs(t, err, confii.ErrConfigLoad)
		assert.Contains(t, err.Error(), `required environment "production" file`)
	})

	t.Run("missing required default", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
sources:
  - type: environment_files
    default_required: true
`, nil)

		_, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.Error(t, err)
		assert.ErrorIs(t, err, confii.ErrConfigLoad)
		assert.Contains(t, err.Error(), "required default file")
	})

	t.Run("unsafe environment", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: ../production
sources:
  - type: environment_files
`, map[string]string{"config/default.yaml": "value: default\n"})

		_, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.Error(t, err)
		assert.ErrorIs(t, err, confii.ErrConfigLoad)
		assert.Contains(t, err.Error(), "unsafe environment name")
	})

	t.Run("invalid template", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: environment_files
    environment_file: production.yaml
`, nil)

		_, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.Error(t, err)
		assert.ErrorIs(t, err, confii.ErrConfigLoad)
		assert.Contains(t, err.Error(), "must contain the {environment} placeholder")
	})
}

func TestEnvironmentFiles_ExistingSourceModesRemainUnchanged(t *testing.T) {
	t.Run("one file with environment sections", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: yaml
    path: application.yaml
`, map[string]string{
			"application.yaml":       "default:\n  port: 8080\n  host: localhost\nproduction:\n  host: api.example.com\n",
			"config/default.yaml":    "port: 9999\n",
			"config/production.yaml": "host: wrong.example.com\n",
		})

		original, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(root))
		t.Cleanup(func() { _ = os.Chdir(original) })

		cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.NoError(t, err)
		assert.Equal(t, 8080, cfg.GetIntOr("port", 0))
		assert.Equal(t, "api.example.com", cfg.GetStringOr("host", ""))
		require.Len(t, cfg.Layers(), 1)
		assert.Equal(t, "application.yaml", cfg.Layers()[0]["source"])
	})

	t.Run("explicit loaders suppress declarative discovery", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: environment_files
`, map[string]string{
			"config/default.yaml":    "value: discovered\n",
			"config/production.yaml": "value: discovered-production\n",
			"explicit.yaml":          "value: explicit\n",
		})

		cfg, err := confii.New[any](context.Background(),
			confii.WithWorkingDir(root),
			confii.WithLoaders(loader.NewYAML(filepath.Join(root, "explicit.yaml"))),
		)
		require.NoError(t, err)
		assert.Equal(t, "explicit", cfg.GetStringOr("value", ""))
		require.Len(t, cfg.Layers(), 1)
	})
}

func TestEnvironmentFiles_IncrementalReloadRefreshesSelectedFiles(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: environment_files
`, map[string]string{
		"config/default.yaml":    "value: default\n",
		"config/production.yaml": "value: before\n",
	})

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "config", "production.yaml"), []byte("value: after\n"), 0o600))
	require.NoError(t, cfg.Reload(context.Background()))
	assert.Equal(t, "after", cfg.GetStringOr("value", ""))
}
