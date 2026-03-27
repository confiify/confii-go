package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// JSONSchemaValidator validates configuration against a JSON Schema.
type JSONSchemaValidator struct {
	schema *jsonschema.Schema
}

// NewJSONSchemaValidator creates a validator from a JSON Schema map.
func NewJSONSchemaValidator(schemaMap map[string]any) (*JSONSchemaValidator, error) {
	data, err := json.Marshal(schemaMap)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	return compileSchema(data)
}

// NewJSONSchemaValidatorFromFile creates a validator from a JSON Schema file.
func NewJSONSchemaValidatorFromFile(path string) (*JSONSchemaValidator, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema file: %w", err)
	}
	return compileSchema(data)
}

func compileSchema(data []byte) (*JSONSchemaValidator, error) {
	var schemaDoc any
	if err := json.Unmarshal(data, &schemaDoc); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schemaDoc); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}
	schema, err := c.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	return &JSONSchemaValidator{schema: schema}, nil
}

// Validate validates the configuration data against the schema.
func (v *JSONSchemaValidator) Validate(data map[string]any) error {
	err := v.schema.Validate(data)
	if err == nil {
		return nil
	}

	ve, ok := err.(*jsonschema.ValidationError)
	if ok {
		var msgs []string
		collectErrors(ve, &msgs)
		return fmt.Errorf("JSON Schema validation failed: %s", strings.Join(msgs, "; "))
	}
	return fmt.Errorf("JSON Schema validation: %w", err)
}

func collectErrors(ve *jsonschema.ValidationError, msgs *[]string) {
	if ve.ErrorKind != nil {
		loc := strings.Join(ve.InstanceLocation, "/")
		if loc == "" {
			loc = "(root)"
		}
		// Use fmt.Sprint on ErrorKind to get a string representation safely.
		*msgs = append(*msgs, fmt.Sprintf("%s: %v", loc, ve.ErrorKind))
	}
	for _, cause := range ve.Causes {
		collectErrors(cause, msgs)
	}
}
