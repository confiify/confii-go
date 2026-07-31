// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"
	"log/slog"
	"reflect"

	"github.com/confiify/confii-go/v2/internal/dictutil"
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
	return c.validateMaterializedCandidate(c.envConfig)
}

// validateMaterializedCandidate applies the complete validation plan to an
// unpublished snapshot. Every lifecycle path uses this method so construction,
// reload, mutation, extension, override, and secret refresh enforce identical
// rules before publication.
func (c *Config[T]) validateMaterializedCandidate(candidate map[string]any) error {
	return c.validateCandidate(candidate, c.opts.ValidateOnLoad)
}

// validateCandidate applies the canonical validation plan when enabled. The
// explicit switch lets Reload override validate_on_load for one transaction
// without maintaining a second implementation of the validation pipeline.
func (c *Config[T]) validateCandidate(candidate map[string]any, enabled bool) error {
	if !enabled {
		return nil
	}

	// JSON Schema is a hard contract when validation-on-load is enabled.
	if c.jsonSchema != nil {
		msgs, err := c.jsonSchema.ValidateDetailed(candidate)
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

	for index, validator := range c.opts.Validators {
		if err := validator.Validate(dictutil.DeepCopy(candidate)); err != nil {
			return &ConfigError{
				Op: "Validate",
				Err: fmt.Errorf(
					"%w: custom validator %d: %w",
					ErrConfigValidation, index, err,
				),
			}
		}
	}

	// A typed Config carries its schema in T. Requiring WithSchema in
	// addition made the README's New[AppConfig](..., WithValidateOnLoad)
	// pattern silently skip validation. Untyped Config[any] values remain
	// a no-op unless a JSON Schema is configured.
	if configTypeSupportsStructValidation[T]() {
		if _, err := validate.DecodeAndValidate[T](candidate); err != nil {
			if c.opts.StrictValidation {
				return NewValidationError([]string{err.Error()}, err)
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
