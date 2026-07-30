// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"
	"log/slog"
	"reflect"

	"github.com/confiify/confii-go/v2/validate"
)

// resolveSchemaValidator compiles an inline JSON Schema or a schema file.
// Inline schemas take precedence over schema paths. Struct schemas are
// validated through the typed configuration path and return no JSON Schema
// validator here.
func resolveSchemaValidator(opts *options) (*validate.JSONSchemaValidator, error) {
	if m, ok := opts.Schema.(map[string]any); ok {
		v, err := validate.NewJSONSchemaValidator(m)
		if err != nil {
			return nil, &ConfigError{
				Op:  "New",
				Err: fmt.Errorf("%w: compile inline JSON schema: %w", ErrConfigValidation, err),
			}
		}
		return v, nil
	}
	if opts.SchemaPath != "" {
		v, err := validate.NewJSONSchemaValidatorFromFile(opts.SchemaPath)
		if err != nil {
			return nil, &ConfigError{
				Op:     "New",
				Source: opts.SchemaPath,
				Err:    fmt.Errorf("%w: load JSON schema from path: %w", ErrConfigValidation, err),
			}
		}
		return v, nil
	}
	return nil, nil
}

// runValidateOnLoad validates the materialized snapshot with JSON Schema and
// typed struct rules. JSON Schema errors are always fatal and expose
// structured violations without configuration values. Non-strict struct
// validation logs violations and allows publication.
func (c *Config[T]) runValidateOnLoad() error {
	if !c.opts.ValidateOnLoad {
		return nil
	}

	// JSON Schema is a hard contract when validation-on-load is enabled.
	if c.jsonSchema != nil {
		msgs, err := c.jsonSchema.ValidateDetailed(c.envConfig)
		if err != nil {
			count := max(1, len(msgs))
			// Sanitized public message: count of violations, no raw
			// values. Full structured detail is on Context.
			return &ConfigError{
				Op: "Validate",
				Err: fmt.Errorf(
					"%w: schema validation failed for %d constraint(s)",
					ErrConfigValidation, count,
				),
				Context: map[string]any{
					"schema_errors": msgs,
				},
			}
		}
	}

	// A typed Config carries its schema in T. Requiring WithSchema in
	// addition made the README's New[AppConfig](..., WithValidateOnLoad)
	// pattern silently skip validation. Untyped Config[any] values remain
	// a no-op unless a JSON Schema is configured.
	if configTypeSupportsStructValidation[T]() {
		if _, err := c.Typed(); err != nil {
			if c.opts.StrictValidation {
				return err
			}
			c.logger.Warn(
				"validation failed on load",
				slog.String("error", err.Error()),
			)
		}
	}

	return nil
}

// configTypeSupportsStructValidation reports whether T has struct-tag
// validation semantics. It deliberately excludes maps/interfaces such as
// Config[any], for which validator.Struct would return InvalidValidationError.
func configTypeSupportsStructValidation[T any]() bool {
	t := reflect.TypeOf((*T)(nil)).Elem()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}
