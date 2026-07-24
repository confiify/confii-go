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

	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// G01: Validate-on-load and schema options are fully wired.
//
// Pre-G01, WithSchemaPath was never read by New (the path was stored on
// opts but no file I/O ever ran), and the validate-on-load gate at New
// only fired when opts.Schema was non-nil — so an inline JSON Schema map
// passed via WithSchema was treated as a presence sentinel rather than
// as a validator. The fix wires both options through a compiled
// jsonschema.JSONSchemaValidator that runs in New, Reload, and Extend
// before commit, with sanitized typed-error surfaces.
//
// This file pins the post-G01 contracts. Each subtest names a distinct
// observable that fails under the pre-fix code; un-skip / removal
// instructions are recorded in the test header where applicable.

const g01ValidSchema = `{
	"type": "object",
	"properties": {
		"port": {"type": "integer", "minimum": 1024, "maximum": 65535}
	},
	"required": ["port"]
}`

// writeSchemaFile is a small helper used by the G01 tests.
func writeSchemaFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

// ---------------------------------------------------------------------------
// SUB-FIX 1: WithSchemaPath actually loads the schema file.
// ---------------------------------------------------------------------------

// TestSchemaPath_LoadedAndEnforced pins:
//
//   - Observable: with a written-to-disk JSON Schema and valid config,
//     New succeeds (the schema file IS read and compiled).
//   - Observable: with the same schema and an invalid config, New
//     returns a typed *ConfigError wrapping ErrConfigValidation.
//
// Pre-fix: SchemaPath was stored on opts but never read; the invalid
// case would succeed silently.
func TestSchemaPath_LoadedAndEnforced(t *testing.T) {
	schemaPath := writeSchemaFile(t, g01ValidSchema)

	t.Run("valid config passes", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 5432}}
		cfg, err := New[any](context.Background(),
			WithLoaders(l),
			WithSchemaPath(schemaPath),
			WithValidateOnLoad(true),
		)
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})

	t.Run("invalid config fails with typed ConfigError", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
		_, err := New[any](context.Background(),
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
		// Sanitized public message: no raw violating value (80) should leak.
		assert.NotContains(t, ce.Error(), " 80 ",
			"public error message must not echo violating value")
	})
}

// TestSchemaPath_FileMissing_TypedError pins:
//
//   - Observable: an unreadable / non-existent schema path surfaces a
//     typed *ConfigError wrapping ErrConfigValidation at New time, not
//     a silent no-op.
//
// Pre-fix: SchemaPath was never read so a missing file produced no
// error — schema-driven validation was a stub.
func TestSchemaPath_FileMissing_TypedError(t *testing.T) {
	l := &stubLoader{source: "s", data: map[string]any{"port": 5432}}
	_, err := New[any](context.Background(),
		WithLoaders(l),
		WithSchemaPath("/nonexistent/schema.json"),
		WithValidateOnLoad(true),
	)
	require.Error(t, err, "missing schema file must surface a typed error")
	assert.True(t, errors.Is(err, ErrConfigValidation),
		"missing schema file must wrap ErrConfigValidation")
}

// ---------------------------------------------------------------------------
// SUB-FIX 2: WithValidateOnLoad(true) + schema runs JSON Schema validation.
// ---------------------------------------------------------------------------

// TestWithSchema_InlineMap_Enforced pins:
//
//   - Observable: when a JSON Schema map is passed via WithSchema and
//     ValidateOnLoad is true, an invalid config produces a typed
//     ConfigError; a valid config succeeds.
//
// Pre-fix: opts.Schema as map[string]any was a non-nil sentinel only.
// The path through Typed() decoded into `any` and silently passed.
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
		cfg, err := New[any](context.Background(),
			WithLoaders(l),
			WithSchema(schema),
			WithValidateOnLoad(true),
		)
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})

	t.Run("invalid config fails with typed ConfigError", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
		_, err := New[any](context.Background(),
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

// TestPublicMessage_SanitizedContextDetailed pins:
//
//   - Observable: the public error string MUST NOT contain the raw
//     violating value. The structured violation list IS available on
//     err.Context["schema_errors"].
//
// Pre-fix: validate-on-load was a stub for JSON Schema dicts. There
// was no Context payload to inspect.
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
	// "hunter2" is the secret-like leaking value; it must NOT appear in
	// any public surface returned by New.
	l := &stubLoader{source: "s", data: map[string]any{
		"db": map[string]any{"password": "hunter2"},
	}}
	_, err := New[any](context.Background(),
		WithLoaders(l),
		WithSchema(schema),
		WithValidateOnLoad(true),
	)
	require.Error(t, err)
	var ce *ConfigError
	require.True(t, errors.As(err, &ce))

	// Sanitized public surface.
	assert.NotContains(t, ce.Error(), "hunter2",
		"public error string must not leak the violating value")

	// Structured detail available for programmatic callers.
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

// ---------------------------------------------------------------------------
// SUB-FIX 4: Reload validates against the schema and rolls back on failure.
// ---------------------------------------------------------------------------

// TestReload_SchemaValidation_Rollback pins:
//
//   - Observable: a v1 file with a valid value loads; rewriting it with
//     a value that violates the JSON Schema causes Reload to return a
//     typed validation error, AND the live state must reflect the v1
//     value (rollback). The Wave 7 G14 / D05 snapshot contract MUST hold.
//
// Pre-fix: Reload's validate phase only ran struct DecodeAndValidate; a
// JSON Schema violation was silently committed.
func TestReload_SchemaValidation_Rollback(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 5432\n"), 0o644))

	schemaPath := writeSchemaFile(t, g01ValidSchema)

	// Build a Config using a fileAutoLoader so Reload can detect file changes.
	cfg, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: cfgPath}),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(true),
	)
	require.NoError(t, err)
	v1, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v1)

	// Rewrite the file with a value that violates the schema.
	require.NoError(t, os.WriteFile(cfgPath, []byte("port: 80\n"), 0o644))

	err = cfg.Reload(context.Background(), WithIncremental(false))
	require.Error(t, err, "Reload must fail when new state violates schema")
	assert.True(t, errors.Is(err, ErrConfigValidation),
		"reload validation error must wrap ErrConfigValidation")

	// Rollback contract (Wave 7 G14): live state still reflects v1.
	v2, _ := cfg.Get("port")
	assert.EqualValues(t, 5432, v2,
		"failed reload must restore the previous envConfig value")
}

// ---------------------------------------------------------------------------
// SUB-FIX 5 + backward compat: WithValidateOnLoad(false) is a no-op even
// when a schema is configured.
// ---------------------------------------------------------------------------

// TestValidateOnLoad_False_NoOp pins:
//
//   - Observable: with ValidateOnLoad=false and an invalid config that
//     would have failed schema validation, New succeeds. The schema is
//     compiled (path is read) but validation is not invoked.
//
// Pre-fix: stub behavior already produced this result, but the contract
// is now documented and tested explicitly so any future regression to
// "validate even when ValidateOnLoad=false" is caught.
func TestValidateOnLoad_False_NoOp(t *testing.T) {
	schemaPath := writeSchemaFile(t, g01ValidSchema)
	// port=80 violates minimum=1024 — would fail with ValidateOnLoad=true.
	l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
	cfg, err := New[any](context.Background(),
		WithLoaders(l),
		WithSchemaPath(schemaPath),
		WithValidateOnLoad(false),
	)
	require.NoError(t, err, "ValidateOnLoad=false must skip schema validation")
	require.NotNil(t, cfg)
}

// TestValidateOnLoad_True_NoSchema_NoOp pins the untyped behavior:
// Config[any] has no struct tags to validate, so validate-on-load remains
// a no-op unless a JSON Schema is supplied.
func TestValidateOnLoad_True_NoSchema_NoOp(t *testing.T) {
	l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
	cfg, err := New[any](context.Background(),
		WithLoaders(l),
		WithValidateOnLoad(true),
	)
	require.NoError(t, err,
		"validate-on-load without a schema must not error")
	require.NotNil(t, cfg)
}

func TestValidateOnLoad_TypedConfigDoesNotRequireWithSchema(t *testing.T) {
	type appConfig struct {
		Port int `mapstructure:"port" validate:"gte=1,lte=65535"`
	}

	l := &stubLoader{source: "s", data: map[string]any{"port": 0}}
	_, err := New[appConfig](context.Background(),
		WithLoaders(l),
		WithValidateOnLoad(true),
		WithStrictValidation(true),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigValidation)
}

// ---------------------------------------------------------------------------
// SUB-FIX 1 (extended): self-config path for schema_path.
// ---------------------------------------------------------------------------

// TestSelfConfig_SchemaPath_EndToEnd pins:
//
//   - Observable: a schema_path declared in .confii.yaml is loaded by
//     applySelfConfig and threaded through New's validate-on-load.
//     Pre-fix the field reached opts.SchemaPath but New never read the
//     file so the self-config-driven workflow was a stub.
func TestSelfConfig_SchemaPath_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(schemaPath, []byte(g01ValidSchema), 0o644))

	// Drop a .confii.yaml in cwd that points at the schema file.
	confiiYAML := filepath.Join(dir, ".confii.yaml")
	require.NoError(t, os.WriteFile(confiiYAML,
		[]byte("schema_path: "+schemaPath+"\nvalidate_on_load: true\n"),
		0o644))

	// Self-config reader looks in the current working directory; chdir
	// for the duration of this test, and ensure the module-level
	// self-config cache is cleared before and after so this test doesn't
	// inherit / leak settings to/from other tests.
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(origWD) }()
	selfconfig.ClearCache()
	defer selfconfig.ClearCache()

	t.Run("self-config schema_path enforces validation", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 80}}
		_, err := New[any](context.Background(), WithLoaders(l))
		require.Error(t, err,
			"self-config schema_path + validate_on_load must enforce schema")
		assert.True(t, errors.Is(err, ErrConfigValidation),
			"self-config-driven failure must wrap ErrConfigValidation")
	})

	t.Run("self-config schema_path passes valid config", func(t *testing.T) {
		l := &stubLoader{source: "s", data: map[string]any{"port": 5432}}
		cfg, err := New[any](context.Background(), WithLoaders(l))
		require.NoError(t, err)
		require.NotNil(t, cfg)
	})
}
