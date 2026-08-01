// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package selfconfig

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestDefaultYAMLDocumentsOnlyCanonicalSourceTypes(t *testing.T) {
	template := string(DefaultYAML())
	for _, canonical := range []string{"environment_files", "yaml", "json", "toml", "ini", "dotenv", "environment"} {
		assert.Contains(t, template, canonical)
	}
	for _, legacy := range []string{"environment-files", "env-vars", "envfile", "type:env\n"} {
		assert.False(t, strings.Contains(template, legacy), "template contains legacy source type %q", legacy)
	}
}

func TestDefaultYAMLCoversEverySetting(t *testing.T) {
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal(DefaultYAML(), &document))
	require.Len(t, document.Content, 1)
	require.Equal(t, yaml.MappingNode, document.Content[0].Kind)

	keys := make(map[string]bool)
	content := document.Content[0].Content
	for i := 0; i < len(content); i += 2 {
		keys[content[i].Value] = true
	}

	settingsType := reflect.TypeOf(Settings{})
	require.Equal(t, settingsType.NumField(), len(keys),
		"the default template and Settings schema must have exactly the same top-level fields")
	for i := 0; i < settingsType.NumField(); i++ {
		field := settingsType.Field(i)
		key := field.Tag.Get("yaml")
		require.NotEmpty(t, key, "Settings.%s must declare a YAML field name", field.Name)
		assert.True(t, keys[key], "default template is missing Settings.%s (%s)", field.Name, key)
	}
}

func TestDefaultYAMLIsSafeAndMatchesBuiltInDefaults(t *testing.T) {
	var settings Settings
	require.NoError(t, yaml.Unmarshal(DefaultYAML(), &settings))

	assert.Empty(t, settings.DefaultEnvironment)
	assert.Empty(t, settings.EnvSwitcher)
	assert.Empty(t, settings.EnvPrefix)
	assert.Equal(t, "deep_merge", settings.Merge.Default)
	assert.Empty(t, settings.Merge.Paths)
	assert.Empty(t, settings.SchemaPath)
	assert.Empty(t, settings.Sources)
	assert.Empty(t, settings.Secrets)
	assert.Equal(t, "auto", settings.EnvironmentStrategy)
	assert.Equal(t, "last_wins", settings.EnvironmentConflictPolicy)
	assert.Equal(t, "raise", settings.OnError)
	assert.Empty(t, settings.LogLevel)
	assert.Equal(t, "150ms", settings.ReloadDebounce)
	assert.Empty(t, settings.SensitivePaths)

	require.NotNil(t, settings.UseEnvExpander)
	assert.True(t, *settings.UseEnvExpander)
	require.NotNil(t, settings.UseFileResolver)
	assert.False(t, *settings.UseFileResolver)
	require.NotNil(t, settings.UseStructuredResolver)
	assert.False(t, *settings.UseStructuredResolver)
	require.NotNil(t, settings.UseURLResolver)
	assert.False(t, *settings.UseURLResolver)
	require.NotNil(t, settings.UseCommandResolver)
	assert.False(t, *settings.UseCommandResolver)
	require.NotNil(t, settings.UseTypeCasting)
	assert.True(t, *settings.UseTypeCasting)

	for name, value := range map[string]*bool{
		"sysenv_fallback":   settings.SysenvFallback,
		"validate_on_load":  settings.ValidateOnLoad,
		"dynamic_reloading": settings.DynamicReloading,
		"freeze_on_load":    settings.FreezeOnLoad,
		"debug_mode":        settings.DebugMode,
	} {
		require.NotNil(t, value, "%s must be explicit in the default template", name)
		assert.False(t, *value, "%s must be disabled in the safe default template", name)
	}
	require.NotNil(t, settings.StrictValidation)
	assert.True(t, *settings.StrictValidation)
}

func TestDefaultYAMLReturnsIndependentCopies(t *testing.T) {
	first := DefaultYAML()
	require.NotEmpty(t, first)
	first[0] = 'x'
	assert.NotEqual(t, first, DefaultYAML())
}
