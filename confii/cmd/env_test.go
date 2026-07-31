// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingEnvironmentWriter struct{}

func (failingEnvironmentWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func writeEnvironmentCommandProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "config"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte(`# keep this comment
default_environment: "development"
env_switcher: "APP_ENV"
environment_strategy: named_files
sources:
  - type: environment_files
    search_paths: [config]
    default_file: default.yaml
    environment_file: "{environment}.yaml"
    default_required: true
    environment_required: true
`), 0o640))
	for name, body := range map[string]string{
		"default.yaml":     "server:\n  port: 8080\n",
		"development.yaml": "log:\n  level: debug\n",
		"production.yaml":  "server:\n  host: api.example.com\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "config", name), []byte(body), 0o600))
	}
	chdirForHelpers(t, root)
	selfconfig.ClearCache()
	t.Cleanup(selfconfig.ClearCache)
	return root
}

func executeEnvironmentCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewEnvCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), err
}

func TestEnvironmentCommandCurrentAndList(t *testing.T) {
	writeEnvironmentCommandProject(t)
	t.Setenv("APP_ENV", "")

	output, err := executeEnvironmentCommand(t)
	require.NoError(t, err)
	assert.Contains(t, output, "Environment: development")
	assert.Contains(t, output, "Selected by: default_environment")
	assert.Contains(t, output, "Strategy: named_files")

	output, err = executeEnvironmentCommand(t, "list")
	require.NoError(t, err)
	assert.Equal(t, "development (current, default)\nproduction\n", output)
}

func TestEnvironmentCommandReportsSwitcherAndJSON(t *testing.T) {
	writeEnvironmentCommandProject(t)
	t.Setenv("APP_ENV", "production")

	output, err := executeEnvironmentCommand(t, "current", "--json")
	require.NoError(t, err)
	var status environmentStatus
	require.NoError(t, json.Unmarshal([]byte(output), &status))
	assert.Equal(t, "production", status.Effective)
	assert.Equal(t, "development", status.ConfiguredDefault)
	assert.Equal(t, "env_switcher", status.SelectedBy)
	assert.Equal(t, []string{"development", "production"}, status.Available)
}

func TestEnvironmentListSurvivesUnknownSwitcherValue(t *testing.T) {
	writeEnvironmentCommandProject(t)
	t.Setenv("APP_ENV", "does-not-exist")

	output, err := executeEnvironmentCommand(t, "list")
	require.NoError(t, err)
	assert.Contains(t, output, "development (default)")
	assert.Contains(t, output, "production")

	output, err = executeEnvironmentCommand(t, "current")
	require.NoError(t, err)
	assert.Contains(t, output, "Environment: does-not-exist")
	assert.Contains(t, output, "Selected by: env_switcher")
}

func TestEnvironmentCommandSetIsSafeAndExplainsOverride(t *testing.T) {
	root := writeEnvironmentCommandProject(t)
	t.Setenv("APP_ENV", "development")

	output, err := executeEnvironmentCommand(t, "set", "production")
	require.NoError(t, err)
	assert.Contains(t, output, "Default environment set to production")
	assert.Contains(t, output, "Effective environment remains development while APP_ENV is set")

	path := filepath.Join(root, ".confii.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# keep this comment")
	assert.Contains(t, string(data), `default_environment: "production"`)
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	}

	output, err = executeEnvironmentCommand(t, "reset")
	require.NoError(t, err)
	assert.Contains(t, output, "Effective environment remains development while APP_ENV is set")
}

func TestEnvironmentCommandSetValidationAndReset(t *testing.T) {
	root := writeEnvironmentCommandProject(t)
	t.Setenv("APP_ENV", "")

	_, err := executeEnvironmentCommand(t, "set", "staging")
	require.ErrorContains(t, err, "is not available")

	_, err = executeEnvironmentCommand(t, "set", "../production", "--allow-unknown")
	require.ErrorContains(t, err, "invalid environment")

	_, err = executeEnvironmentCommand(t, "set", "staging", "--allow-unknown")
	require.NoError(t, err)
	output, err := executeEnvironmentCommand(t, "reset")
	require.NoError(t, err)
	assert.Contains(t, output, "Default environment cleared")
	data, readErr := os.ReadFile(filepath.Join(root, ".confii.yaml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `default_environment: ""`)
}

func TestEnvironmentCommandRequiresInitializedProject(t *testing.T) {
	chdirForHelpers(t, t.TempDir())
	selfconfig.ClearCache()
	_, err := executeEnvironmentCommand(t)
	require.ErrorContains(t, err, "run `confii init` first")
}

func TestReplaceTopLevelSettingRejectsMissingAndDuplicate(t *testing.T) {
	updated, err := replaceTopLevelSetting([]byte("env_switcher: APP_ENV\n"), "default_environment:", `"dev"`)
	require.NoError(t, err)
	assert.Equal(t, "env_switcher: APP_ENV\ndefault_environment: \"dev\"\n", string(updated))

	_, err = replaceTopLevelSetting([]byte("default_environment: dev\ndefault_environment: prod\n"), "default_environment:", `"qa"`)
	require.ErrorContains(t, err, "more than once")

	updated, err = replaceTopLevelSetting([]byte("default_environment=\"dev # literal\" # preserve\r\n"), "default_environment", `"prod"`)
	require.NoError(t, err)
	assert.Equal(t, "default_environment = \"prod\" # preserve\r\n", string(updated))
}

func TestEnvironmentCommandNoAvailableEnvironments(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte("default_environment: \"\"\n"), 0o600))
	chdirForHelpers(t, root)
	selfconfig.ClearCache()

	output, err := executeEnvironmentCommand(t, "list")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(output, "No environments were discovered"))
}

func TestEnvironmentCommandListsSectionedEnvironments(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "config"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte(`
default_environment: staging
env_switcher: APP_ENV
environment_strategy: sectioned
sources:
  - type: yaml
    path: config/application.yaml
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "config", "application.yaml"), []byte(`
default:
  server:
    port: 8080
development:
  log:
    level: debug
staging:
  server:
    host: staging.example.com
production:
  server:
    host: api.example.com
`), 0o600))
	chdirForHelpers(t, root)
	selfconfig.ClearCache()
	t.Setenv("APP_ENV", "")

	output, err := executeEnvironmentCommand(t, "list")
	require.NoError(t, err)
	assert.Equal(t, "development\nproduction\nstaging (current, default)\n", output)
}

func TestEnvironmentListJSONAndIdempotentMutations(t *testing.T) {
	writeEnvironmentCommandProject(t)
	t.Setenv("APP_ENV", "")

	output, err := executeEnvironmentCommand(t, "list", "--json")
	require.NoError(t, err)
	var status environmentStatus
	require.NoError(t, json.Unmarshal([]byte(output), &status))
	assert.Equal(t, []string{"development", "production"}, status.Available)

	output, err = executeEnvironmentCommand(t, "set", "development")
	require.NoError(t, err)
	assert.Contains(t, output, "already development")

	_, err = executeEnvironmentCommand(t, "reset")
	require.NoError(t, err)
	output, err = executeEnvironmentCommand(t, "reset")
	require.NoError(t, err)
	assert.Contains(t, output, "already empty")
}

func TestEnvironmentSubcommandsPropagateInspectionErrors(t *testing.T) {
	chdirForHelpers(t, t.TempDir())
	selfconfig.ClearCache()
	for _, args := range [][]string{{"current"}, {"list"}, {"set", "production"}, {"reset"}} {
		_, err := executeEnvironmentCommand(t, args...)
		require.ErrorContains(t, err, "run `confii init` first")
	}
}

func TestEnvironmentCommandRejectsAmbiguousAndInvalidSelfConfig(t *testing.T) {
	t.Run("ambiguous", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "confii.yaml"), []byte("default_environment: dev\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte("default_environment: dev\n"), 0o600))
		chdirForHelpers(t, root)
		selfconfig.ClearCache()
		_, err := executeEnvironmentCommand(t)
		require.ErrorContains(t, err, "multiple Confii self-configuration files")
	})
	t.Run("invalid", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte("unknown_setting: true\n"), 0o600))
		chdirForHelpers(t, root)
		selfconfig.ClearCache()
		_, err := executeEnvironmentCommand(t)
		require.ErrorContains(t, err, "unknown_setting")
	})
}

func TestPrintEnvironmentStatusEmptyValuesAndWriteError(t *testing.T) {
	cmd := NewEnvCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, printEnvironmentStatus(cmd, environmentStatus{SelectedBy: "none", Strategy: "auto"}, false))
	assert.Contains(t, output.String(), "Environment: (none)")
	assert.Contains(t, output.String(), "Configured default: (none)")
	assert.Contains(t, output.String(), "Switcher: (not configured)")

	cmd.SetOut(failingEnvironmentWriter{})
	require.ErrorContains(t, printEnvironmentStatus(cmd, environmentStatus{}, false), "write failed")
	require.ErrorContains(t, printEnvironmentStatus(cmd, environmentStatus{}, true), "write failed")
}

func TestUpdateDefaultEnvironmentFormatsAndRejections(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name     string
		filename string
		input    string
		want     string
	}{
		{name: "yaml", filename: ".confii.yml", input: "default_environment: dev # retained\n", want: `default_environment: "prod" # retained`},
		{name: "toml", filename: ".confii.toml", input: "default_environment=\"dev\" # retained\n", want: `default_environment = "prod" # retained`},
		{name: "json", filename: ".confii.json", input: "{\"env_switcher\":\"APP_ENV\",\"default_environment\":\"dev\"}\n", want: `"default_environment": "prod"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.filename)
			require.NoError(t, os.WriteFile(path, []byte(test.input), 0o600))
			require.NoError(t, updateDefaultEnvironment(path, "prod"))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(data), test.want)
		})
	}

	require.Error(t, updateDefaultEnvironment(filepath.Join(root, "missing.yaml"), "prod"))
	directory := filepath.Join(root, "directory.yaml")
	require.NoError(t, os.Mkdir(directory, 0o750))
	require.ErrorContains(t, updateDefaultEnvironment(directory, "prod"), "non-regular")
	badJSON := filepath.Join(root, "bad.json")
	require.NoError(t, os.WriteFile(badJSON, []byte("{"), 0o600))
	require.ErrorContains(t, updateDefaultEnvironment(badJSON, "prod"), "update default_environment")
	unsupported := filepath.Join(root, "confii.txt")
	require.NoError(t, os.WriteFile(unsupported, []byte("default_environment=dev\n"), 0o600))
	require.ErrorContains(t, updateDefaultEnvironment(unsupported, "prod"), "unsupported self-config format")
}

func TestAtomicReplacementFallbackHappyPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, ".confii.yaml")
	temporary := filepath.Join(root, ".replacement")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(temporary, []byte("new"), 0o600))
	require.NoError(t, replaceFileWithBackup(temporary, target))
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))

	require.Error(t, atomicReplaceFile(filepath.Join(root, "missing", ".confii.yaml"), []byte("new"), 0o600))
}

func TestTopLevelSettingCommentQuotedHashesAndEscapes(t *testing.T) {
	assert.Equal(t, " # outside", topLevelSettingComment(`default_environment: "dev \"#literal" # outside`))
	assert.Empty(t, topLevelSettingComment(`default_environment: 'dev # literal'`))
}

func TestEnvironmentCommandInspectionAndWriterFailures(t *testing.T) {
	t.Run("missing required shared file", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, ".confii.yaml"), []byte(`
default_environment: development
sources:
  - type: environment_files
    search_paths: [config]
    default_required: true
`), 0o600))
		chdirForHelpers(t, root)
		selfconfig.ClearCache()
		_, err := executeEnvironmentCommand(t, "set", "production")
		require.ErrorContains(t, err, "verify environment before setting it")
	})

	t.Run("list writer", func(t *testing.T) {
		writeEnvironmentCommandProject(t)
		t.Setenv("APP_ENV", "")
		cmd := NewEnvCmd()
		cmd.SetOut(failingEnvironmentWriter{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"list"})
		require.ErrorContains(t, cmd.Execute(), "write failed")
	})

	t.Run("set writer", func(t *testing.T) {
		writeEnvironmentCommandProject(t)
		t.Setenv("APP_ENV", "")
		cmd := NewEnvCmd()
		cmd.SetOut(failingEnvironmentWriter{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"set", "production"})
		require.ErrorContains(t, cmd.Execute(), "write failed")
	})
}
