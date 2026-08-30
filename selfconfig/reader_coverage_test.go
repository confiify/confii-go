// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package selfconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRead_EmptyDirDefaultsToCWD(t *testing.T) {
	ClearCache()

	_, err := Read("")
	assert.NoError(t, err)
}

func TestRead_CacheBehaviorForCWD(t *testing.T) {
	ClearCache()

	s1, err := Read(".")
	require.NoError(t, err)

	s2, err := Read(".")
	require.NoError(t, err)

	assert.Equal(t, s1, s2)
}

func TestClearCache_ResetsState(t *testing.T) {

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "confii.yaml"),
		[]byte(`default_environment: cache-test`), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	ClearCache()
	cacheMu.Lock()
	assert.Empty(t, cache, "cache must be empty before first Read")
	cacheMu.Unlock()

	first, err := Read(".")
	require.NoError(t, err)
	require.NotNil(t, first, "Read must surface the temp dir's confii.yaml")
	assert.Equal(t, "cache-test", first.DefaultEnvironment)

	absKey, err := filepath.Abs(".")
	require.NoError(t, err)

	cacheMu.Lock()
	assert.Len(t, cache, 1, "cache must hold exactly one entry after Read(\".\")")
	entry, ok := cache[cacheKeyValue{directory: absKey}]
	assert.True(t, ok, "cache key must be filepath.Abs(\".\") ")
	require.NotNil(t, entry.settings, "cache must hold the result pointer")
	cacheMu.Unlock()

	second, err := Read(".")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.NotSame(t, first, second,
		"cached reads must return caller-owned Settings copies")
	assert.Equal(t, first, second)

	ClearCache()
	cacheMu.Lock()
	assert.Empty(t, cache, "cache must be empty after ClearCache")
	cacheMu.Unlock()

	third, err := Read(".")
	require.NoError(t, err)
	require.NotNil(t, third)
	assert.NotSame(t, first, third,
		"after ClearCache(), Read must produce a freshly-allocated *Settings")
	assert.Equal(t, "cache-test", third.DefaultEnvironment,
		"fresh read must still surface the on-disk value")
}

func TestRead_YMLExtension(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
default_environment: yml-test
log_level: debug
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "yml-test", settings.DefaultEnvironment)
	assert.Equal(t, "debug", settings.LogLevel)
}

func TestRead_HiddenYMLFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
env_switcher: MY_ENV_VAR
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "MY_ENV_VAR", settings.EnvSwitcher)
}

func TestRead_HiddenJSONFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"default_environment": "hidden-json", "log_level": "warn"}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.json"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "hidden-json", settings.DefaultEnvironment)
	assert.Equal(t, "warn", settings.LogLevel)
}

func TestRead_HiddenTOMLFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
default_environment = "hidden-toml"
schema_path = "/etc/schema.json"
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.toml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "hidden-toml", settings.DefaultEnvironment)
	assert.Equal(t, "/etc/schema.json", settings.SchemaPath)
}

func TestRead_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`invalid: yaml: [broken`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), content, 0644))

	ClearCache()
	_, err := Read(dir)
	assert.Error(t, err)
}

func TestRead_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{invalid json}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"), content, 0644))

	ClearCache()
	_, err := Read(dir)
	assert.Error(t, err)
}

func TestRead_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`[broken toml
invalid`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.toml"), content, 0644))

	ClearCache()
	_, err := Read(dir)
	assert.Error(t, err)
}

func TestRead_JSONWithAllBooleanFields(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{
		"sysenv_fallback":true,
		"merge":{"default":"shallow_merge"},
		"validate_on_load":true,
		"strict_validation":false,
		"reject_unknown_keys":false,
		"use_env_expander":true,
		"use_file_resolver":true,
		"use_structured_resolver":true,
		"use_url_resolver":true,
		"use_command_resolver":true,
		"use_type_casting":false,
		"dynamic_reloading":true,
		"reload_debounce":"250ms",
		"sensitive_paths":["database.password"],
		"freeze_on_load":false,
		"debug_mode":true
	}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)

	require.NotNil(t, settings.SysenvFallback)
	assert.True(t, *settings.SysenvFallback)
	assert.Equal(t, "shallow_merge", settings.Merge.Default)
	require.NotNil(t, settings.ValidateOnLoad)
	assert.True(t, *settings.ValidateOnLoad)
	require.NotNil(t, settings.StrictValidation)
	assert.False(t, *settings.StrictValidation)
	require.NotNil(t, settings.UseEnvExpander)
	assert.True(t, *settings.UseEnvExpander)
	require.NotNil(t, settings.UseFileResolver)
	assert.True(t, *settings.UseFileResolver)
	require.NotNil(t, settings.UseStructuredResolver)
	assert.True(t, *settings.UseStructuredResolver)
	require.NotNil(t, settings.UseURLResolver)
	assert.True(t, *settings.UseURLResolver)
	require.NotNil(t, settings.UseCommandResolver)
	assert.True(t, *settings.UseCommandResolver)
	require.NotNil(t, settings.UseTypeCasting)
	assert.False(t, *settings.UseTypeCasting)
	require.NotNil(t, settings.DynamicReloading)
	assert.True(t, *settings.DynamicReloading)
	assert.Equal(t, "250ms", settings.ReloadDebounce)
	assert.Equal(t, []string{"database.password"}, settings.SensitivePaths)
	require.NotNil(t, settings.FreezeOnLoad)
	assert.False(t, *settings.FreezeOnLoad)
	require.NotNil(t, settings.DebugMode)
	assert.True(t, *settings.DebugMode)
}

func TestRead_TOMLWithSources(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
default_environment = "staging"

[[sources]]
type = "file"
path = "base.toml"

[[sources]]
type = "environment"
prefix = "APP"
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.toml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.Len(t, settings.Sources, 2)
	assert.Equal(t, "file", settings.Sources[0]["type"])
	assert.Equal(t, "environment", settings.Sources[1]["type"])
}

func TestRead_PriorityYAMLOverJSON(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte(`default_environment: from-yaml`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"),
		[]byte(`{"default_environment": "from-json"}`), 0644))

	ClearCache()
	settings, err := Read(dir)
	require.Error(t, err)
	assert.Nil(t, settings)
	assert.Contains(t, err.Error(), "multiple self-config formats")
}

func TestRead_PriorityPrimaryOverHidden(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.toml"),
		[]byte(`default_environment = "primary"`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.toml"),
		[]byte(`default_environment = "hidden"`), 0644))

	ClearCache()
	settings, err := Read(dir)
	require.Error(t, err)
	assert.Nil(t, settings)
	assert.Contains(t, err.Error(), "hidden and visible")
}

func TestRead_NonCWDIsCachedByAbsPath(t *testing.T) {
	ClearCache()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"),
		[]byte(`{"default_environment":"nocache","sensitive_paths":["database.password"]}`), 0644))

	first, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, first)

	absKey, err := filepath.Abs(dir)
	require.NoError(t, err)

	cacheMu.Lock()
	entry, ok := cache[cacheKeyValue{directory: absKey}]
	assert.True(t, ok, "non-CWD Read must populate the cache keyed by abs path ")
	assert.NotSame(t, first, entry.settings,
		"Read must not expose the cache-owned Settings pointer")
	assert.Equal(t, first, entry.settings)
	cacheMu.Unlock()

	second, err := Read(dir)
	require.NoError(t, err)
	assert.NotSame(t, first, second,
		"second Read must return a detached copy of the cached Settings")
	assert.Equal(t, first, second)
	first.SensitivePaths[0] = "caller.changed"
	third, err := Read(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"database.password"}, third.SensitivePaths,
		"caller mutation must not reach cached sensitive-path policy")
}

func TestRead_XDGConfigFallback(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, ".config", "confii")
	require.NoError(t, os.MkdirAll(xdg, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(xdg, "confii.yaml"),
		[]byte("default_environment: from_xdg\n"), 0o644))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	ClearCache()
	t.Cleanup(ClearCache)

	// With no project self-config the XDG fallback is authoritative.
	dir := t.TempDir()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "from_xdg", settings.DefaultEnvironment)

	// A project self-config takes precedence over the XDG fallback.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"),
		[]byte("default_environment: from_project\n"), 0o644))
	ClearCache()
	settings, err = Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "from_project", settings.DefaultEnvironment)
}

func TestRead_EmptyConfigFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(``), 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)

	require.NotNil(t, settings)
	assert.Equal(t, "", settings.DefaultEnvironment)
}
