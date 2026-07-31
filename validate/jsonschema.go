// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

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

// NewJSONSchemaValidator compiles schemaMap as JSON Schema. The map must be
// JSON-serializable and use keywords supported by the underlying draft
// implementation. Compilation errors are returned before a validator is
// created.
func NewJSONSchemaValidator(schemaMap map[string]any) (*JSONSchemaValidator, error) {
	data, err := json.Marshal(schemaMap)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	return compileSchema(data)
}

// NewJSONSchemaValidatorFromFile reads and compiles a JSON Schema document from
// path. Relative paths are resolved by the process working directory. Read,
// JSON decoding, resource registration, and compilation failures are returned.
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

// Validate validates data against the compiled schema. It returns nil on
// success or an aggregated error containing schema locations and failed
// keywords. Violating configuration values are not included in the generated
// message.
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
// structured error context.
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
	// Report leaf constraint violations and omit wrapper nodes that do not
	// identify a failed schema keyword.
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
