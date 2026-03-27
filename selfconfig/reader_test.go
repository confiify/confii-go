package selfconfig

import (
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
