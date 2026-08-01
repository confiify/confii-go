// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelfConfigAppliesReloadDebounceAndSensitivePaths(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, ".confii.yaml"), []byte(`
reload_debounce: 275ms
sensitive_paths:
  - credentials.token
  - database.password
`), 0o600))
	selfconfig.ClearCache()
	options := defaultOptions()
	options.WorkingDir = directory
	require.NoError(t, applySelfConfig(&options))
	assert.Equal(t, 275*time.Millisecond, options.ReloadDebounce)
	assert.Equal(t, []string{"credentials.token", "database.password"}, options.SensitivePaths)
}

func TestSelfConfigRejectsInvalidReloadDebounce(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, ".confii.yaml"), []byte("reload_debounce: immediate\n"), 0o600))
	selfconfig.ClearCache()
	options := defaultOptions()
	options.WorkingDir = directory
	err := applySelfConfig(&options)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
}

func TestSelfConfigAccountsForEveryStartupOption(t *testing.T) {
	declarative := map[string][]string{
		"Env":                         {"DefaultEnvironment"},
		"EnvSwitcher":                 {"EnvSwitcher"},
		"Loaders":                     {"Sources"},
		"DynamicReloading":            {"DynamicReloading"},
		"ReloadDebounce":              {"ReloadDebounce"},
		"UseEnvExpander":              {"UseEnvExpander"},
		"UseFileResolver":             {"UseFileResolver"},
		"UseStructuredResolver":       {"UseStructuredResolver"},
		"UseURLResolver":              {"UseURLResolver"},
		"UseCommandResolver":          {"UseCommandResolver"},
		"UseTypeCasting":              {"UseTypeCasting"},
		"MergeStrategy":               {"Merge"},
		"MergeStrategyMap":            {"Merge"},
		"EnvPrefix":                   {"EnvPrefix"},
		"SysenvFallback":              {"SysenvFallback"},
		"SchemaPath":                  {"SchemaPath"},
		"ValidateOnLoad":              {"ValidateOnLoad"},
		"StrictValidation":            {"StrictValidation"},
		"FreezeOnLoad":                {"FreezeOnLoad"},
		"OnError":                     {"OnError"},
		"DebugMode":                   {"DebugMode"},
		"EnvironmentStrategy":         {"EnvironmentStrategy"},
		"EnvironmentConflictPolicy":   {"EnvironmentConflictPolicy"},
		"StartupTimeout":              {"Startup"},
		"OperationTimeout":            {"Runtime"},
		"SecretResolutionConcurrency": {"SecretResolutionConcurrency"},
		"SensitivePaths":              {"SensitivePaths"},
	}
	intentionalCodeOnly := map[string]string{
		"WorkingDir":     "selects the directory in which self-config is discovered",
		"SecretResolver": "programmatic resolver wiring; declarative providers use Secrets",
		"valueResolvers": "holds application-defined Go value resolver functions",
		"Exporters":      "holds application-defined Go serializers",
		"Validators":     "holds application-defined Go validation rules",
		"Schema":         "holds an in-memory Go value; declarative schemas use SchemaPath",
		"Logger":         "holds a Go logger object; declarative logging uses LogLevel",
		"SecretHook":     "holds a Go function; declarative secret hooks use Secrets",
		"hookSetups":     "holds Go hook functions frozen into the materialization plan",
	}
	internalBookkeeping := map[string]bool{
		"environmentConflictPolicyConfigured": true,
		"selfConfigSources":                   true,
		"selfConfigSecrets":                   true,
		"selfConfigSecretProvider":            true,
		"selfConfigSecretProviders":           true,
		"explicitlySet":                       true,
	}

	settingsType := reflect.TypeOf(selfconfig.Settings{})
	settingFields := make(map[string]bool, settingsType.NumField())
	for i := 0; i < settingsType.NumField(); i++ {
		settingFields[settingsType.Field(i).Name] = true
	}

	optionsType := reflect.TypeOf(options{})
	for i := 0; i < optionsType.NumField(); i++ {
		name := optionsType.Field(i).Name
		settings, isDeclarative := declarative[name]
		_, isCodeOnly := intentionalCodeOnly[name]
		isInternal := internalBookkeeping[name]
		assert.Equal(t, 1, boolCount(isDeclarative, isCodeOnly, isInternal),
			"options.%s must have exactly one self-config classification", name)
		for _, setting := range settings {
			assert.True(t, settingFields[setting],
				"options.%s maps to missing selfconfig.Settings.%s", name, setting)
		}
	}

	require.NotEmpty(t, intentionalCodeOnly["WorkingDir"])
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}
