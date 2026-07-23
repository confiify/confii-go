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
	// #nosec G304 -- path is the caller-selected schema file; arbitrary paths are this API's contract.
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

// ValidateDetailed validates the configuration data against the schema and
// returns the list of structured violation messages alongside the aggregated
// error. The msgs slice contains one entry per leaf-level constraint
// violation in the form "<instance/location>: <kind>" where kind is the
// JSON Schema keyword that failed (e.g. "minimum", "required", "pattern").
// The returned messages reference schema constraint metadata only — they
// do not echo the violating user-supplied values — so callers may safely
// surface them in operator-facing diagnostics or attach them to a
// structured error context (G01).
//
// Returns (nil, nil) when data satisfies the schema.
func (v *JSONSchemaValidator) ValidateDetailed(data map[string]any) ([]string, error) {
	err := v.schema.Validate(data)
	if err == nil {
		return nil, nil
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if ok {
		var msgs []string
		collectErrors(ve, &msgs)
		return msgs, fmt.Errorf("JSON Schema validation failed: %s", strings.Join(msgs, "; "))
	}
	return nil, fmt.Errorf("JSON Schema validation: %w", err)
}

func collectErrors(ve *jsonschema.ValidationError, msgs *[]string) {
	// Only emit a message for nodes that own a non-zero KeywordPath. The
	// jsonschema library wraps every failure in a top-level "Schema"
	// node whose ErrorKind has an empty KeywordPath; emitting that node
	// adds a noisy "(root): &{file://...}" entry that doesn't carry a
	// constraint name. Recursing through Causes finds the leaf
	// constraint-violation entries that actually identify the failed
	// keyword (e.g. "minimum", "required", "pattern").
	if ve.ErrorKind != nil {
		kw := ve.ErrorKind.KeywordPath()
		if len(kw) > 0 {
			loc := "/" + strings.Join(ve.InstanceLocation, "/")
			if len(ve.InstanceLocation) == 0 {
				loc = "(root)"
			}
			// Format: "<instance/location>: <keyword>" — schema constraint
			// metadata only, no raw user values, so the message is safe to
			// surface in operator-facing diagnostics.
			*msgs = append(*msgs, fmt.Sprintf("%s: %s", loc, strings.Join(kw, "/")))
		}
	}
	for _, cause := range ve.Causes {
		collectErrors(cause, msgs)
	}
}
