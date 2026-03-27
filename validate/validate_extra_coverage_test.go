package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Struct validator with nested optional fields
// ---------------------------------------------------------------------------

type OptionalNested struct {
	Name     string `mapstructure:"name" validate:"required"`
	Optional string `mapstructure:"optional" validate:"omitempty,min=3"`
}

func TestStructValidator_Validate_OptionalFieldEmpty(t *testing.T) {
	v := NewStructValidator[OptionalNested]()

	// Optional field not provided -- should be valid.
	err := v.Validate(map[string]any{"name": "test"})
	assert.NoError(t, err)
}

func TestStructValidator_Validate_OptionalFieldTooShort(t *testing.T) {
	v := NewStructValidator[OptionalNested]()

	// Optional field provided but too short.
	err := v.Validate(map[string]any{"name": "test", "optional": "ab"})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// DecodeAndValidate with nil map
// ---------------------------------------------------------------------------

func TestDecodeAndValidate_NilMap(t *testing.T) {
	type Simple struct {
		Name string `mapstructure:"name"`
	}

	result, err := DecodeAndValidate[Simple](nil)
	require.NoError(t, err)
	assert.Equal(t, "", result.Name)
}

// ---------------------------------------------------------------------------
// Decode with nested struct and missing fields
// ---------------------------------------------------------------------------

type ParentConfig struct {
	Child ChildConfig `mapstructure:"child"`
}

type ChildConfig struct {
	Value string `mapstructure:"value" validate:"required"`
}

func TestDecodeAndValidate_NestedMissingRequired(t *testing.T) {
	data := map[string]any{
		"child": map[string]any{},
	}
	_, err := DecodeAndValidate[ParentConfig](data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}

func TestDecode_NestedStruct(t *testing.T) {
	data := map[string]any{
		"child": map[string]any{"value": "hello"},
	}
	result, err := Decode[ParentConfig](data)
	require.NoError(t, err)
	assert.Equal(t, "hello", result.Child.Value)
}

// ---------------------------------------------------------------------------
// JSON Schema validator - collectErrors with nested causes
// ---------------------------------------------------------------------------

func TestJSONSchemaValidator_ValidateDeepNesting(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"level1": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"level2": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"value": map[string]any{"type": "string"},
						},
						"required": []any{"value"},
					},
				},
				"required": []any{"level2"},
			},
		},
		"required": []any{"level1"},
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	// Valid.
	err = v.Validate(map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{"value": "ok"},
		},
	})
	assert.NoError(t, err)

	// Missing deeply nested required field.
	err = v.Validate(map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Schema validation")
}

// ---------------------------------------------------------------------------
// JSON Schema from file with TOML and YAML-like content (invalid)
// ---------------------------------------------------------------------------

func TestJSONSchemaValidatorFromFile_ValidSchema(t *testing.T) {
	dir := t.TempDir()
	schema := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"count": {"type": "integer", "minimum": 0}
		},
		"required": ["name"]
	}`
	path := filepath.Join(dir, "valid_schema.json")
	require.NoError(t, os.WriteFile(path, []byte(schema), 0644))

	v, err := NewJSONSchemaValidatorFromFile(path)
	require.NoError(t, err)

	assert.NoError(t, v.Validate(map[string]any{"name": "test", "count": 5}))
	assert.NoError(t, v.Validate(map[string]any{"name": "test"}))
	assert.Error(t, v.Validate(map[string]any{"count": 5}))
	assert.Error(t, v.Validate(map[string]any{"name": "test", "count": -1}))
}

// ---------------------------------------------------------------------------
// Struct validator with boolean and numeric edge cases
// ---------------------------------------------------------------------------

type NumericConfig struct {
	Min int     `mapstructure:"min" validate:"min=0"`
	Max int     `mapstructure:"max" validate:"max=100"`
	F   float64 `mapstructure:"f" validate:"required"`
}

func TestStructValidator_NumericBoundaries(t *testing.T) {
	v := NewStructValidator[NumericConfig]()

	assert.NoError(t, v.Validate(map[string]any{"min": 0, "max": 100, "f": 1.5}))
	assert.NoError(t, v.Validate(map[string]any{"min": 50, "max": 50, "f": 0.1}))
	assert.Error(t, v.Validate(map[string]any{"min": -1, "max": 50, "f": 1.0}))
	assert.Error(t, v.Validate(map[string]any{"min": 0, "max": 101, "f": 1.0}))
}

// ---------------------------------------------------------------------------
// Decode with completely wrong type (should use weakly typed input)
// ---------------------------------------------------------------------------

func TestDecode_WeaklyTyped_BoolFromString(t *testing.T) {
	type BoolConfig struct {
		Enabled bool `mapstructure:"enabled"`
	}

	result, err := Decode[BoolConfig](map[string]any{"enabled": "true"})
	require.NoError(t, err)
	assert.True(t, result.Enabled)
}

func TestDecode_WeaklyTyped_IntFromFloat(t *testing.T) {
	type IntConfig struct {
		Port int `mapstructure:"port"`
	}

	result, err := Decode[IntConfig](map[string]any{"port": 8080.0})
	require.NoError(t, err)
	assert.Equal(t, 8080, result.Port)
}

// Try to trigger decode error in StructValidator.Validate by passing
// a value that mapstructure cannot handle even with WeaklyTypedInput.
func TestStructValidator_Validate_DecodeErrorPath(t *testing.T) {
	type StrictTypes struct {
		Items []string `mapstructure:"items" validate:"required"`
	}
	v := NewStructValidator[StrictTypes]()

	// This should decode successfully due to WeaklyTypedInput.
	err := v.Validate(map[string]any{"items": []any{"a", "b"}})
	assert.NoError(t, err)

	// Missing required field.
	err = v.Validate(map[string]any{})
	assert.Error(t, err)
}

// Test the decode internal function edge cases via exported wrappers.
func TestDecode_MapWithSliceValues(t *testing.T) {
	type SliceConfig struct {
		Items []int `mapstructure:"items"`
	}
	result, err := Decode[SliceConfig](map[string]any{"items": []any{1, 2, 3}})
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, result.Items)
}

func TestDecodeAndValidate_MapWithSliceValues(t *testing.T) {
	type SliceConfig struct {
		Items []int `mapstructure:"items" validate:"required"`
	}
	result, err := DecodeAndValidate[SliceConfig](map[string]any{"items": []any{1, 2, 3}})
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, result.Items)
}

// ---------------------------------------------------------------------------
// JSON Schema Validate with non-ValidationError (edge case)
// ---------------------------------------------------------------------------

func TestJSONSchemaValidator_ValidateValidData(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	// Passing valid data should return nil error.
	err = v.Validate(map[string]any{"anything": "goes"})
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// StructValidator with data that can't be decoded (edge case)
// ---------------------------------------------------------------------------

func TestStructValidator_Validate_EmptyData(t *testing.T) {
	v := NewStructValidator[TestConfig]()

	// Empty map will decode with zero values, then validation catches missing required fields.
	err := v.Validate(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}

// ---------------------------------------------------------------------------
// Decode with nil map
// ---------------------------------------------------------------------------

func TestDecode_NilMap(t *testing.T) {
	type Simple struct {
		Name string `mapstructure:"name"`
	}

	result, err := Decode[Simple](nil)
	require.NoError(t, err)
	assert.Equal(t, "", result.Name)
}

// ---------------------------------------------------------------------------
// JSON Schema from map with complex valid schema
// ---------------------------------------------------------------------------

func TestJSONSchemaValidator_ComplexSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":      "string",
				"minLength": float64(1),
				"maxLength": float64(50),
			},
			"age": map[string]any{
				"type":    "integer",
				"minimum": float64(0),
				"maximum": float64(150),
			},
			"tags": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required": []any{"name"},
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	assert.NoError(t, v.Validate(map[string]any{
		"name": "test",
		"age":  30,
		"tags": []any{"a", "b"},
	}))

	// Name too long.
	longName := ""
	for i := 0; i < 51; i++ {
		longName += "x"
	}
	assert.Error(t, v.Validate(map[string]any{"name": longName}))

	// Missing required name.
	assert.Error(t, v.Validate(map[string]any{"age": 30}))

	// Wrong type for age.
	assert.Error(t, v.Validate(map[string]any{"name": "test", "age": "not-a-number"}))
}
