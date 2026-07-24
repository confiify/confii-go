package confii_test

import (
	"bytes"
	"context"
	"log/slog"
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

func TestEnvironmentStrategy_InferredNamedFilesRejectsSectionedSource(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`, map[string]string{
		"application.yaml":       "default:\n  server:\n    port: 7000\nproduction:\n  server:\n    host: application.example.com\n",
		"config/default.yaml":    "server:\n  port: 8080\n",
		"config/production.yaml": "server:\n  host: named.example.com\n",
	})

	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(original) })

	_, err = confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
	assert.Contains(t, err.Error(), `environment_strategy "named_files"`)
	assert.Contains(t, err.Error(), `explicitly select "hybrid"`)
}

func TestEnvironmentStrategy_NamedFilesAllowsFlatSourcesAndBuildsPlan(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`, map[string]string{
		"application.yaml":       "feature:\n  enabled: true\n",
		"config/default.yaml":    "server:\n  port: 8080\n",
		"config/production.yaml": "server:\n  host: named.example.com\n",
	})

	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(original) })

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	assert.True(t, cfg.GetBoolOr("feature.enabled", false))
	assert.Equal(t, 8080, cfg.GetIntOr("server.port", 0))
	assert.Equal(t, "named.example.com", cfg.GetStringOr("server.host", ""))

	plan := cfg.SourcePlan()
	assert.Equal(t, confii.EnvironmentStrategyNamedFiles, plan.Strategy)
	assert.Equal(t, confii.EnvironmentConflictLastWins, plan.ConflictPolicy)
	require.Len(t, plan.Layers, 3)
	assert.Equal(t, []string{"flat", "default", "environment"}, []string{
		plan.Layers[0].Role,
		plan.Layers[1].Role,
		plan.Layers[2].Role,
	})
	assert.Empty(t, plan.Conflicts)
}

func TestEnvironmentStrategy_AutoReportsEffectiveSectionedPlan(t *testing.T) {
	root := writeEnvironmentFilesProject(t, `
default_environment: production
sources:
  - type: yaml
    path: application.yaml
`, map[string]string{
		"application.yaml": "default:\n  server:\n    port: 8080\nproduction:\n  server:\n    host: api.example.com\n",
	})

	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(original) })

	cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.GetIntOr("server.port", 0))
	assert.Equal(t, "api.example.com", cfg.GetStringOr("server.host", ""))

	plan := cfg.SourcePlan()
	assert.Equal(t, confii.EnvironmentStrategySectioned, plan.Strategy)
	require.Len(t, plan.Layers, 1)
	assert.Equal(t, "sectioned", plan.Layers[0].Role)
}

func TestEnvironmentStrategy_HybridConflictPoliciesAndPrecedence(t *testing.T) {
	files := map[string]string{
		"application.yaml":       "default:\n  server:\n    port: 7000\nproduction:\n  server:\n    host: application.example.com\n",
		"config/default.yaml":    "server:\n  port: 8080\n",
		"config/production.yaml": "server:\n  host: named.example.com\n",
	}

	t.Run("error", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
environment_strategy: hybrid
environment_conflict_policy: error
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`, files)
		original, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(root))
		t.Cleanup(func() { _ = os.Chdir(original) })

		_, err = confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hybrid environment sources write the same keys")
		assert.Contains(t, err.Error(), "server.host")
		assert.Contains(t, err.Error(), "server.port")
	})

	t.Run("last wins", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
environment_strategy: hybrid
environment_conflict_policy: last_wins
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`, files)
		original, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(root))
		t.Cleanup(func() { _ = os.Chdir(original) })

		cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.NoError(t, err)
		assert.Equal(t, 8080, cfg.GetIntOr("server.port", 0))
		assert.Equal(t, "named.example.com", cfg.GetStringOr("server.host", ""))

		plan := cfg.SourcePlan()
		assert.Equal(t, confii.EnvironmentStrategyHybrid, plan.Strategy)
		assert.Equal(t, confii.EnvironmentConflictLastWins, plan.ConflictPolicy)
		require.Len(t, plan.Conflicts, 2)
		assert.Equal(t, "server.host", plan.Conflicts[0].Key)
		assert.Equal(t, filepath.Join(root, "config", "production.yaml"), plan.Conflicts[0].LastWriter)
		assert.Equal(t, "server.port", plan.Conflicts[1].Key)
		assert.Equal(t, filepath.Join(root, "config", "default.yaml"), plan.Conflicts[1].LastWriter)

		plan.Layers[0].Keys[0] = "mutated"
		plan.Conflicts[0].Sources[0] = "mutated"
		fresh := cfg.SourcePlan()
		assert.NotEqual(t, "mutated", fresh.Layers[0].Keys[0])
		assert.NotEqual(t, "mutated", fresh.Conflicts[0].Sources[0])
	})

	t.Run("warn", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
environment_strategy: hybrid
environment_conflict_policy: warn
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`, files)
		original, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(root))
		t.Cleanup(func() { _ = os.Chdir(original) })

		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		cfg, err := confii.New[any](context.Background(),
			confii.WithWorkingDir(root),
			confii.WithLogger(logger),
		)
		require.NoError(t, err)
		assert.Equal(t, "named.example.com", cfg.GetStringOr("server.host", ""))
		assert.Contains(t, logs.String(), "hybrid environment source conflicts")
		assert.Contains(t, logs.String(), "server.host")
	})

	t.Run("declared source order remains authoritative", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
environment_strategy: hybrid
environment_conflict_policy: last_wins
sources:
  - type: environment_files
  - type: yaml
    path: application.yaml
`, files)
		original, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(root))
		t.Cleanup(func() { _ = os.Chdir(original) })

		cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.NoError(t, err)
		assert.Equal(t, 7000, cfg.GetIntOr("server.port", 0))
		assert.Equal(t, "application.example.com", cfg.GetStringOr("server.host", ""))
	})

	t.Run("non-overlapping models are valid", func(t *testing.T) {
		root := writeEnvironmentFilesProject(t, `
default_environment: production
environment_strategy: hybrid
environment_conflict_policy: error
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`, map[string]string{
			"application.yaml":       "default:\n  server:\n    port: 7000\nproduction:\n  server:\n    host: application.example.com\n",
			"config/default.yaml":    "feature:\n  enabled: true\n",
			"config/production.yaml": "database:\n  host: database.example.com\n",
		})
		original, err := os.Getwd()
		require.NoError(t, err)
		require.NoError(t, os.Chdir(root))
		t.Cleanup(func() { _ = os.Chdir(original) })

		cfg, err := confii.New[any](context.Background(), confii.WithWorkingDir(root))
		require.NoError(t, err)
		assert.Equal(t, 7000, cfg.GetIntOr("server.port", 0))
		assert.True(t, cfg.GetBoolOr("feature.enabled", false))
		assert.Equal(t, "database.example.com", cfg.GetStringOr("database.host", ""))
		assert.Empty(t, cfg.SourcePlan().Conflicts)
	})
}

func TestEnvironmentStrategy_ConfigurationValidation(t *testing.T) {
	tests := []struct {
		name       string
		selfConfig string
		files      map[string]string
		message    string
	}{
		{
			name:       "invalid strategy",
			selfConfig: "environment_strategy: everything\n",
			message:    "invalid environment_strategy",
		},
		{
			name:       "invalid conflict policy",
			selfConfig: "environment_conflict_policy: silent\n",
			message:    "invalid environment_conflict_policy",
		},
		{
			name: "sectioned rejects environment files",
			selfConfig: `
environment_strategy: sectioned
sources:
  - type: environment_files
`,
			message: "cannot be combined",
		},
		{
			name:       "named files requires source",
			selfConfig: "environment_strategy: named_files\n",
			message:    "requires an environment_files source",
		},
		{
			name: "hybrid requires explicit policy",
			selfConfig: `
environment_strategy: hybrid
sources:
  - type: environment_files
`,
			message: "requires an explicit environment_conflict_policy",
		},
		{
			name: "hybrid requires loaded sectioned source",
			selfConfig: `
default_environment: production
environment_strategy: hybrid
environment_conflict_policy: last_wins
sources:
  - type: yaml
    path: application.yaml
  - type: environment_files
`,
			files: map[string]string{
				"application.yaml":       "feature: true\n",
				"config/default.yaml":    "server:\n  port: 8080\n",
				"config/production.yaml": "server:\n  host: api.example.com\n",
			},
			message: "requires both a loaded environment_files layer and a section-based environment source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeEnvironmentFilesProject(t, tt.selfConfig, tt.files)
			original, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.Chdir(root))
			t.Cleanup(func() { _ = os.Chdir(original) })

			_, err = confii.New[any](context.Background(), confii.WithWorkingDir(root))
			require.Error(t, err)
			assert.ErrorIs(t, err, confii.ErrConfigLoad)
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}
