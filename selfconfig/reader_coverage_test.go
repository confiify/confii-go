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

// TestClearCache_ResetsState exercises the cache lifecycle end-to-end:
//  1. After ClearCache, the cache map is empty.
//  2. After Read("."), the cache map has exactly one entry keyed by the
//     absolute path of the CWD (G06 changed the key from literal "." to
//     filepath.Abs(dir)).
//  3. A second Read(".") returns the SAME pointer (cache hit, not a re-read).
//  4. After ClearCache, the cache map is empty again AND the next Read
//     produces a fresh result (cache invalidation works).
func TestClearCache_ResetsState(t *testing.T) {
	// Use a temp directory so we are not affected by ambient confii.* files
	// in the actual CWD. We Chdir into it so Read(".") finds OUR file.
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "confii.yaml"),
		[]byte(`default_environment: cache-test`), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// Step 1: starting state is empty.
	ClearCache()
	cacheMu.Lock()
	assert.Empty(t, cache, "cache must be empty before first Read")
	cacheMu.Unlock()

	// Step 2: Read(".") populates the cache with one entry keyed by abs.
	first, err := Read(".")
	require.NoError(t, err)
	require.NotNil(t, first, "Read must surface the temp dir's confii.yaml")
	assert.Equal(t, "cache-test", first.DefaultEnvironment)

	absKey, err := filepath.Abs(".")
	require.NoError(t, err)

	cacheMu.Lock()
	assert.Len(t, cache, 1, "cache must hold exactly one entry after Read(\".\")")
	entry, ok := cache[absKey]
	assert.True(t, ok, "cache key must be filepath.Abs(\".\") (G06)")
	require.NotNil(t, entry.settings, "cache must hold the result pointer")
	cacheMu.Unlock()

	// Step 3: second Read(".") is a cache hit (same pointer, not a re-read).
	second, err := Read(".")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Same(t, first, second,
		"second Read must return the cached *Settings pointer, not a re-read")

	// Step 4: ClearCache invalidates and the next Read produces a fresh
	// pointer (proves cache invalidation, not just state reset).
	ClearCache()
	cacheMu.Lock()
	assert.Empty(t, cache, "cache must be empty after ClearCache")
	cacheMu.Unlock()

	third, err := Read(".")
	require.NoError(t, err)
	require.NotNil(t, third)
	assert.NotSame(t, first, third,
		"after ClearCache, Read must produce a freshly-allocated *Settings")
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

// TestRead_NonCWDIsCachedByAbsPath pins the G06 contract: every Read,
// not just Read("."), is cached, and the cache key is the absolute path
// of the dir argument. Pre-G06 only the literal "." key was memoized so
// non-CWD reads bypassed the cache entirely; post-G06 every distinct
// working directory gets its own cache slot.
func TestRead_NonCWDIsCachedByAbsPath(t *testing.T) {
	ClearCache()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.json"),
		[]byte(`{"default_environment": "nocache"}`), 0644))

	first, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, first)

	absKey, err := filepath.Abs(dir)
	require.NoError(t, err)

	cacheMu.Lock()
	entry, ok := cache[absKey]
	assert.True(t, ok, "non-CWD Read must populate the cache keyed by abs path (G06)")
	assert.Same(t, first, entry.settings,
		"cache must hold the same *Settings pointer Read returned")
	cacheMu.Unlock()

	// Second Read of the same dir returns the same pointer (cache hit).
	second, err := Read(dir)
	require.NoError(t, err)
	assert.Same(t, first, second,
		"second Read of the same dir must return the cached *Settings pointer")
}

func TestRead_XDGConfigFallback(t *testing.T) {
	// Create a temp dir that has no confii config files,
	// so readFromDir falls through to XDG fallback.
	dir := t.TempDir()

	ClearCache()
	// Reading from a dir with no config files should not error.
	settings, err := Read(dir)
	require.NoError(t, err)
	// Settings may be nil if no XDG config exists either.
	_ = settings
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
