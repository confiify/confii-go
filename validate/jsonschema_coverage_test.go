package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONSchemaValidator_ValidateNestedObject(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"database": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{"type": "string"},
					"port": map[string]any{"type": "integer"},
				},
				"required": []any{"host"},
			},
		},
		"required": []any{"database"},
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	// Valid.
	err = v.Validate(map[string]any{
		"database": map[string]any{"host": "localhost", "port": 5432},
	})
	assert.NoError(t, err)

	// Missing nested required field.
	err = v.Validate(map[string]any{
		"database": map[string]any{"port": 5432},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Schema validation")

	// Missing top-level required field.
	err = v.Validate(map[string]any{})
	assert.Error(t, err)
}

func TestJSONSchemaValidator_ValidateAdditionalProperties(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	err = v.Validate(map[string]any{"name": "test", "extra": "field"})
	assert.Error(t, err)
}

func TestJSONSchemaValidator_ValidateEnum(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"level": map[string]any{
				"type": "string",
				"enum": []any{"debug", "info", "warn", "error"},
			},
		},
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	assert.NoError(t, v.Validate(map[string]any{"level": "info"}))
	assert.Error(t, v.Validate(map[string]any{"level": "verbose"}))
}

func TestJSONSchemaValidator_ValidateMinMaxProperties(t *testing.T) {
	schema := map[string]any{
		"type":          "object",
		"minProperties": float64(1),
		"maxProperties": float64(3),
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	assert.Error(t, v.Validate(map[string]any{}))
	assert.NoError(t, v.Validate(map[string]any{"a": 1}))
	assert.NoError(t, v.Validate(map[string]any{"a": 1, "b": 2, "c": 3}))
	assert.Error(t, v.Validate(map[string]any{"a": 1, "b": 2, "c": 3, "d": 4}))
}

func TestJSONSchemaValidatorFromFile(t *testing.T) {
	dir := t.TempDir()
	schemaContent := `{
		"type": "object",
		"properties": {
			"host": {"type": "string"},
			"port": {"type": "integer"}
		},
		"required": ["host"]
	}`
	path := filepath.Join(dir, "schema.json")
	require.NoError(t, os.WriteFile(path, []byte(schemaContent), 0644))

	v, err := NewJSONSchemaValidatorFromFile(path)
	require.NoError(t, err)

	assert.NoError(t, v.Validate(map[string]any{"host": "localhost"}))
	assert.Error(t, v.Validate(map[string]any{"port": 5432}))
}

func TestJSONSchemaValidatorFromFile_NotFound(t *testing.T) {
	_, err := NewJSONSchemaValidatorFromFile("/nonexistent/schema.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read schema file")
}

func TestJSONSchemaValidatorFromFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0644))

	_, err := NewJSONSchemaValidatorFromFile(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal schema")
}

func TestJSONSchemaValidator_ValidatePatternProperty(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"email": map[string]any{
				"type":    "string",
				"pattern": "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$",
			},
		},
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	assert.NoError(t, v.Validate(map[string]any{"email": "test@example.com"}))
	assert.Error(t, v.Validate(map[string]any{"email": "not-an-email"}))
}

func TestJSONSchemaValidator_ValidateArrayItems(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "string"},
				"minItems": float64(1),
			},
		},
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	assert.NoError(t, v.Validate(map[string]any{"tags": []any{"tag1", "tag2"}}))
	assert.Error(t, v.Validate(map[string]any{"tags": []any{}}))
	assert.Error(t, v.Validate(map[string]any{"tags": []any{123}}))
}

func TestNewJSONSchemaValidator_UnmarshalableSchema(t *testing.T) {
	// A schema map that includes a channel, which can't be marshaled to JSON.
	schema := map[string]any{
		"type":   "object",
		"broken": make(chan int),
	}
	_, err := NewJSONSchemaValidator(schema)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal schema")
}
