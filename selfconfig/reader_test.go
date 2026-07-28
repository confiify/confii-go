// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package selfconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRead_YAMLFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
default_environment: production
env_prefix: APP
deep_merge: true
debug_mode: true
environment_strategy: hybrid
environment_conflict_policy: warn
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "production", settings.DefaultEnvironment)
	assert.Equal(t, "APP", settings.EnvPrefix)
	require.NotNil(t, settings.DebugMode)
	assert.True(t, *settings.DebugMode)
	require.NotNil(t, settings.DeepMerge)
	assert.True(t, *settings.DeepMerge)
	assert.Equal(t, "hybrid", settings.EnvironmentStrategy)
	assert.Equal(t, "warn", settings.EnvironmentConflictPolicy)
}

func TestRead_HiddenYAMLFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
default_environment: staging
sysenv_fallback: true
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "staging", settings.DefaultEnvironment)
	require.NotNil(t, settings.SysenvFallback)
	assert.True(t, *settings.SysenvFallback)
}

func TestRead_NoFile(t *testing.T) {
	dir := t.TempDir()
	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	assert.Nil(t, settings)
}

func TestRead_JSONFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"default_environment": "staging", "env_prefix": "TEST"}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "staging", settings.DefaultEnvironment)
	assert.Equal(t, "TEST", settings.EnvPrefix)
}

func TestRead_TOMLFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
default_environment = "dev"
freeze_on_load = true
on_error = "warn"
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.toml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "dev", settings.DefaultEnvironment)
	require.NotNil(t, settings.FreezeOnLoad)
	assert.True(t, *settings.FreezeOnLoad)
	assert.Equal(t, "warn", settings.OnError)
}

func TestRead_DefaultFiles(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
default_files:
  - config/base.yaml
  - config/dev.yaml
default_prefix: MYAPP
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, []string{"config/base.yaml", "config/dev.yaml"}, settings.DefaultFiles)
	assert.Equal(t, "MYAPP", settings.DefaultPrefix)
}

func TestRead_Sources(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
sources:
  - type: yaml
    path: config.yaml
  - type: environment
    prefix: APP
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.Len(t, settings.Sources, 2)
	assert.Equal(t, "yaml", settings.Sources[0]["type"])
	assert.Equal(t, "config.yaml", settings.Sources[0]["path"])
	assert.Equal(t, "environment", settings.Sources[1]["type"])
}

func TestRead_Secrets(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
secrets:
  provider: aws_secrets_manager
  region_name: us-east-1
  cache_enabled: true
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "aws_secrets_manager", settings.Secrets["provider"])
	assert.Equal(t, "us-east-1", settings.Secrets["region_name"])
}

func TestRead_PriorityOrder(t *testing.T) {
	dir := t.TempDir()

	// Both confii.yaml and .confii.yaml exist — confii.yaml wins.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte(`default_environment: from-primary`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"),
		[]byte(`default_environment: from-hidden`), 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "from-primary", settings.DefaultEnvironment)
}

func TestRead_CacheBehavior(t *testing.T) {
	ClearCache()

	// Read from a temp dir (not ".") so cache doesn't interfere.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte(`default_environment: cached`), 0644))

	s1, _ := Read(dir)
	require.NotNil(t, s1)

	// CWD cache only applies to dir=".".
	s2, _ := Read(dir)
	require.NotNil(t, s2)
	assert.Equal(t, s1.DefaultEnvironment, s2.DefaultEnvironment)
}

func TestClearCache(t *testing.T) {
	ClearCache()
	// Should not panic.
	ClearCache()
}

func TestCandidateFilenamesReturnsIndependentDiscoveryOrder(t *testing.T) {
	first := CandidateFilenames()
	require.NotEmpty(t, first)
	assert.Equal(t, "confii.yaml", first[0])
	first[0] = "mutated"
	assert.Equal(t, "confii.yaml", CandidateFilenames()[0])
}

func TestReadRejectsUnknownTopLevelFieldsInEveryFormat(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
	}{
		{name: "yaml", file: "confii.yaml", content: "env_swticher: APP_ENV\n"},
		{name: "json", file: "confii.json", content: `{"env_swticher":"APP_ENV"}`},
		{name: "toml", file: "confii.toml", content: `env_swticher = "APP_ENV"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0644))
			ClearCache()
			t.Cleanup(ClearCache)

			settings, err := Read(dir)
			require.Error(t, err)
			assert.Nil(t, settings)
			assert.Contains(t, err.Error(), "env_swticher")
		})
	}
}

func TestReadRejectsTrailingDocumentsAndValues(t *testing.T) {
	tests := []struct {
		file    string
		content string
	}{
		{file: "confii.yaml", content: "default_environment: development\n---\ndefault_environment: production\n"},
		{file: "confii.json", content: `{"default_environment":"development"} {"default_environment":"production"}`},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0644))
			ClearCache()
			t.Cleanup(ClearCache)
			_, err := Read(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "exactly one")
		})
	}
}

func TestReadRejectsMalformedInputInEveryFormat(t *testing.T) {
	tests := []struct {
		file    string
		content string
	}{
		{file: "confii.yaml", content: "sources: [\n"},
		{file: "confii.json", content: `{"default_environment":`},
		{file: "confii.toml", content: `default_environment = [`},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0644))
			ClearCache()
			t.Cleanup(ClearCache)
			_, err := Read(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "parse self-config")
		})
	}
}

func TestStrictDecodersReturnTrailingSyntaxErrors(t *testing.T) {
	var settings Settings
	err := decodeYAML([]byte("default_environment: development\n---\n["), &settings)
	require.Error(t, err)
	err = decodeJSON([]byte(`{"default_environment":"development"} {`), &settings)
	require.Error(t, err)
}

func TestReadStrictSchemaPreservesProviderSpecificMaps(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
	}{
		{name: "yaml", file: "confii.yaml", content: `
sources:
  - type: custom_provider
    provider_specific_option: preserved
secrets:
  provider: custom_provider
  provider_specific_option: preserved
`},
		{name: "json", file: "confii.json", content: `{"sources":[{"type":"custom_provider","provider_specific_option":"preserved"}],"secrets":{"provider":"custom_provider","provider_specific_option":"preserved"}}`},
		{name: "toml", file: "confii.toml", content: `
[[sources]]
type = "custom_provider"
provider_specific_option = "preserved"

[secrets]
provider = "custom_provider"
provider_specific_option = "preserved"
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0644))
			ClearCache()
			t.Cleanup(ClearCache)

			settings, err := Read(dir)
			require.NoError(t, err)
			require.Len(t, settings.Sources, 1)
			assert.Equal(t, "preserved", settings.Sources[0]["provider_specific_option"])
			assert.Equal(t, "preserved", settings.Secrets["provider_specific_option"])
		})
	}
}

func TestReadAllowsCommentOnlyYAMLTemplate(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte("# configure Confii here\n"), 0644))
	ClearCache()
	t.Cleanup(ClearCache)

	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Empty(t, settings.DefaultEnvironment)
}

func TestReadReportsSelfConfigInspectionFailure(t *testing.T) {
	if testing.Short() || os.PathSeparator == '\\' {
		t.Skip("self-referential symlink behavior is platform-specific")
	}
	dir := t.TempDir()
	require.NoError(t, os.Symlink("confii.yaml", filepath.Join(dir, "confii.yaml")))
	ClearCache()
	t.Cleanup(ClearCache)

	settings, err := Read(dir)
	require.Error(t, err)
	assert.Nil(t, settings)
	assert.Contains(t, err.Error(), "inspect self-config")
}

func TestReadFirstFromDirReportsNotFound(t *testing.T) {
	settings, found, err := readFirstFromDir(t.TempDir())
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, settings)
}

func TestReadUsesXDGStyleHomeFallback(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, ".config", "confii")
	require.NoError(t, os.MkdirAll(xdg, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "confii.yaml"), []byte("default_environment: fallback\n"), 0644))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	ClearCache()
	t.Cleanup(ClearCache)

	settings, err := Read(t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "fallback", settings.DefaultEnvironment)
}

func TestReadWithoutHomeStillTreatsAbsenceAsValid(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	ClearCache()
	t.Cleanup(ClearCache)

	settings, err := Read(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, settings)
}

func TestReadFileReportsReadAndExtensionErrors(t *testing.T) {
	_, err := readFile(filepath.Join(t.TempDir(), "confii.yaml"))
	require.Error(t, err)

	path := filepath.Join(t.TempDir(), "confii.txt")
	require.NoError(t, os.WriteFile(path, []byte("default_environment: development\n"), 0644))
	_, err = readFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported self-config extension")
}
