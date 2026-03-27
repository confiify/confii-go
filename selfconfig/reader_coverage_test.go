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
	// Reading with empty dir defaults to "." (CWD).
	// This may or may not find a config depending on CWD, but should not error.
	_, err := Read("")
	assert.NoError(t, err)
}

func TestRead_CacheBehaviorForCWD(t *testing.T) {
	ClearCache()

	// First call sets the cache.
	s1, err := Read(".")
	require.NoError(t, err)

	// Second call should return the cached result.
	s2, err := Read(".")
	require.NoError(t, err)

	// Both should be the same pointer (or both nil).
	assert.Equal(t, s1, s2)
}

func TestClearCache_ResetsState(t *testing.T) {
	ClearCache()

	// Load something into cache.
	_, _ = Read(".")
	assert.True(t, cacheLoaded || !cacheLoaded) // just ensure no panic

	ClearCache()

	cacheMu.Lock()
	assert.False(t, cacheLoaded)
	assert.Nil(t, cachedResult)
	assert.Equal(t, "", cachedDir)
	cacheMu.Unlock()
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
		"sysenv_fallback": true,
		"deep_merge": false,
		"validate_on_load": true,
		"strict_validation": false,
		"use_env_expander": true,
		"use_type_casting": false,
		"dynamic_reloading": true,
		"freeze_on_load": false,
		"debug_mode": true
	}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)

	require.NotNil(t, settings.SysenvFallback)
	assert.True(t, *settings.SysenvFallback)
	require.NotNil(t, settings.DeepMerge)
	assert.False(t, *settings.DeepMerge)
	require.NotNil(t, settings.ValidateOnLoad)
	assert.True(t, *settings.ValidateOnLoad)
	require.NotNil(t, settings.StrictValidation)
	assert.False(t, *settings.StrictValidation)
	require.NotNil(t, settings.UseEnvExpander)
	assert.True(t, *settings.UseEnvExpander)
	require.NotNil(t, settings.UseTypeCasting)
	assert.False(t, *settings.UseTypeCasting)
	require.NotNil(t, settings.DynamicReloading)
	assert.True(t, *settings.DynamicReloading)
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
type = "env"
prefix = "APP"
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.toml"), content, 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.Len(t, settings.Sources, 2)
	assert.Equal(t, "file", settings.Sources[0]["type"])
	assert.Equal(t, "env", settings.Sources[1]["type"])
}

func TestRead_PriorityYAMLOverJSON(t *testing.T) {
	dir := t.TempDir()

	// confii.yaml should take priority over confii.json.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte(`default_environment: from-yaml`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"),
		[]byte(`{"default_environment": "from-json"}`), 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "from-yaml", settings.DefaultEnvironment)
}

func TestRead_PriorityPrimaryOverHidden(t *testing.T) {
	dir := t.TempDir()

	// confii.toml (primary) should win over .confii.toml (hidden).
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.toml"),
		[]byte(`default_environment = "primary"`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.toml"),
		[]byte(`default_environment = "hidden"`), 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.Equal(t, "primary", settings.DefaultEnvironment)
}

func TestRead_NonCWDNotCached(t *testing.T) {
	ClearCache()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"),
		[]byte(`{"default_environment": "nocache"}`), 0644))

	_, _ = Read(dir)

	// Cache state should not be set for non-CWD reads.
	cacheMu.Lock()
	assert.False(t, cacheLoaded)
	cacheMu.Unlock()
}

func TestRead_EmptyConfigFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(``), 0644))

	ClearCache()
	settings, err := Read(dir)
	require.NoError(t, err)
	// Empty YAML returns a zero-value Settings.
	require.NotNil(t, settings)
	assert.Equal(t, "", settings.DefaultEnvironment)
}
