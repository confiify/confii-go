// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const g01ValidSchema = `{
	"type": "object",
	"properties": {
		"port": {"type": "integer", "minimum": 1024, "maximum": 65535}
	},
	"required": ["port"]
}`

func writeSchemaFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func TestSchemaPath_LoadedAndEnforced(t *testing.T) {
	schemaPath := writeSchemaFile(t, g01ValidSchema)

	t.Run("valid config passes", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 5432}}
		cfg, err := NewWithContext[any](context.Background(),
			WithLoaders(l),
			WithSchemaPath(schemaPath),
			WithValidateOnLoad(true),
		)
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})

	t.Run("invalid config fails with typed ConfigError", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
		_, err := NewWithContext[any](context.Background(),
			WithLoaders(l),
			WithSchemaPath(schemaPath),
			WithValidateOnLoad(true),
		)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrConfigValidation),
			"must wrap ErrConfigValidation sentinel")
		var ce *ConfigError
		require.True(t, errors.As(err, &ce), "must be *ConfigError")
		assert.Equal(t, "Validate", ce.Op)

		assert.NotContains(t, ce.Error(), " 80 ",
			"public error message must not echo violating value")
	})
}

func TestSchemaPath_FileMissing_TypedError(t *testing.T) {
	l := &stubLoader{source: "s", data: map[string]any{"port": 5432}}
	_, err := NewWithContext[any](context.Background(),
		WithLoaders(l),
		WithSchemaPath("/nonexistent/schema.json"),
		WithValidateOnLoad(true),
	)
	require.Error(t, err, "missing schema file must surface a typed error")
	assert.True(t, errors.Is(err, ErrConfigValidation),
		"missing schema file must wrap ErrConfigValidation")
}

func TestWithSchema_InlineMap_Enforced(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"port": map[string]any{"type": "integer", "minimum": 1024},
		},
		"required": []any{"port"},
	}

	t.Run("valid config passes", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 8080}}
		cfg, err := NewWithContext[any](context.Background(),
			WithLoaders(l),
			WithSchema(schema),
			WithValidateOnLoad(true),
		)
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})

	t.Run("invalid config fails with typed ConfigError", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
		_, err := NewWithContext[any](context.Background(),
			WithLoaders(l),
			WithSchema(schema),
			WithValidateOnLoad(true),
		)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrConfigValidation),
			"must wrap ErrConfigValidation")
		var ce *ConfigError
		require.True(t, errors.As(err, &ce), "must be *ConfigError")
		raw, ok := ce.Context["schema_errors"].([]string)
		require.True(t, ok, "Context must carry schema_errors []string")
		require.NotEmpty(t, raw, "schema_errors must list at least one violation")
		joined := strings.Join(raw, " ")
		assert.Contains(t, joined, "minimum",
			"schema_errors must reference the violated keyword")
	})
}

func TestPublicMessage_SanitizedContextDetailed(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"db": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"password": map[string]any{
						"type":      "string",
						"minLength": 16,
					},
				},
				"required": []any{"password"},
			},
		},
	}

	l := &stubLoader{source: "s", data: map[string]any{
		"db": map[string]any{"password": "hunter2"},
	}}
	_, err := NewWithContext[any](context.Background(),
		WithLoaders(l),
		WithSchema(schema),
		WithValidateOnLoad(true),
	)
	require.Error(t, err)
	var ce *ConfigError
	require.True(t, errors.As(err, &ce))

	assert.NotContains(t, ce.Error(), "hunter2",
		"public error string must not leak the violating value")

	raw, ok := ce.Context["schema_errors"].([]string)
	require.True(t, ok)
	require.NotEmpty(t, raw)
	joined := strings.Join(raw, " ")
	assert.Contains(t, joined, "minLength",
		"context must carry the violated keyword for programmatic callers")
	for _, m := range raw {
		assert.NotContains(t, m, "hunter2",
			"individual schema_errors entries must not leak raw values")
	}
}

func TestReload_SchemaValidation_Rollback(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 5432\n"), 0o644))

	schemaPath := writeSchemaFile(t, g01ValidSchema)

	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: cfgPath}),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(true),
	)
	require.NoError(t, err)
	v1, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v1)

	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 80\n"), 0o644))

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.Error(t, err, "Reload must fail when new state violates schema")
	assert.True(t, errors.Is(err, ErrConfigValidation),
		"reload validation error must wrap ErrConfigValidation")

	v2, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v2,
		"failed reload must restore the previous envConfig value")
}

func TestValidateOnLoad_False_NoOp(t *testing.T) {
	schemaPath := writeSchemaFile(t, g01ValidSchema)

	l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(l),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(false),
	)
	require.NoError(t, err, "ValidateOnLoad=false must skip schema validation")
	require.NotNil(t, cfg)
}

func TestValidateOnLoad_True_NoSchema_NoOp(t *testing.T) {
	l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(l),
		WithValidateOnLoad(true),
	)
	require.NoError(t, err,
		"validate-on-load without a schema must not error")
	require.NotNil(t, cfg)
}

func TestValidateOnLoad_TypedConfigDoesNotRequireWithSchema(t *testing.T) {
	type appConfig struct {
		Port int `confii:"port" validate:"gte=1,lte=65535"`
	}

	l := &stubLoader{source: "s", data: map[string]any{"port": 0}}
	_, err := NewWithContext[appConfig](context.Background(),
		WithLoaders(l),
		WithValidateOnLoad(true),
		WithStrictValidation(true),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigValidation)
}

func TestSelfConfig_SchemaPath_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schemaPath, []byte(g01ValidSchema), 0o644))

	confiiYAML := filepath.Join(dir, ".confii.yaml")
	require.NoError(t, os.WriteFile(confiiYAML,
		[]byte("schema_path: "+schemaPath+"\nvalidate_on_load: true\n"),
		0o644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origWD) }()
	selfconfig.ClearCache()
	defer selfconfig.ClearCache()

	t.Run("self-config schema_path enforces validation", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
		_, err := NewWithContext[any](context.Background(), WithLoaders(l))
		require.Error(t, err,
			"self-config schema_path + validate_on_load must enforce schema")
		assert.True(t, errors.Is(err, ErrConfigValidation),
			"self-config-driven failure must wrap ErrConfigValidation")
	})

	t.Run("self-config schema_path passes valid config", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 5432}}
		cfg, err := NewWithContext[any](context.Background(), WithLoaders(l))
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
}
