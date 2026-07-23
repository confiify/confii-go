package confii

import (
	"fmt"
	"log/slog"
	"reflect"

	"github.com/confiify/confii-go/validate"
)

// resolveSchemaValidator compiles a JSON Schema validator from the option
// state, if one is configured. It is the single source of truth for how
// [WithSchema] and [WithSchemaPath] are interpreted by the validate-on-load
// pipeline (G01). The resolution order is:
//
//  1. opts.Schema, when its concrete type is map[string]any: compile via
//     [validate.NewJSONSchemaValidator].
//  2. opts.SchemaPath, when non-empty AND opts.Schema is not already a
//     map[string]any: read the file and compile via
//     [validate.NewJSONSchemaValidatorFromFile]. SchemaPath is honored
//     only when no inline JSON Schema is provided so that the typical
//     "either-or" caller intent is preserved without ambiguity.
//  3. Anything else (struct value, nil, primitive sentinel): return
//     (nil, nil). Struct-shaped schemas drive [Config.Typed]'s
//     mapstructure decode + validator.v10 path; the absence of a JSON
//     Schema validator is the documented signal that struct validation
//     applies instead.
//
// Compile failures (malformed schema map, missing/invalid file) are
// surfaced as a typed [*ConfigError] wrapping [ErrConfigValidation] so
// callers can detect them with [errors.Is] / [errors.As].
func resolveSchemaValidator(opts *options) (*validate.JSONSchemaValidator, error) {
	if m, ok := opts.Schema.(map[string]any); ok {
		v, err := validate.NewJSONSchemaValidator(m)
		if err != nil {
			return nil, &ConfigError{
				Op:  "New",
				Err: fmt.Errorf("%w: compile inline JSON schema: %v", ErrConfigValidation, err),
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
				Err:    fmt.Errorf("%w: load JSON schema from path: %v", ErrConfigValidation, err),
			}
		}
		return v, nil
	}
	return nil, nil
}

// runValidateOnLoad executes the validate-on-load pipeline for a freshly
// loaded envConfig. It is called from both [New] (via the Step 6 hook
// site) and from the [Config.Reload] / [Config.Extend] validate phases so
// that the three lifecycles agree on which validator runs and how its
// errors are surfaced (G01).
//
// Two validators may run, in this order:
//
//  1. JSON Schema validator (when c.jsonSchema is non-nil): the resolved
//     envConfig is validated against the compiled schema. Failures
//     return a typed [*ConfigError] wrapping [ErrConfigValidation] with
//     a sanitized public message ("schema validation failed for N
//     constraint(s)") and the full structured violation list on
//     Context["schema_errors"]. The raw values of violating keys are
//     never embedded in the public error message — programmatic callers
//     read Context.
//  2. Struct validator (when opts.Schema is a non-nil non-map value):
//     [validate.DecodeAndValidate] runs the existing struct-tag
//     validation. Failures are wrapped via [NewValidationError] so the
//     ErrConfigValidation sentinel chain is preserved.
//
// When both validators are configured (a JSON Schema map + a struct
// type, an unusual combination), JSON Schema runs first because it
// gates structural correctness before mapstructure decode, which can
// otherwise mask schema-level violations behind type-cast errors.
//
// Honors [WithStrictValidation]: when strict is false, the legacy
// behavior (warn-and-continue on Typed-style validation) is preserved
// for the struct path. JSON Schema violations always return an error
// when validate-on-load is true regardless of strict mode, because the
// schema is a hard contract — a non-strict downgrade would be the same
// silent-stub behavior G01 was filed to remove.
func (c *Config[T]) runValidateOnLoad() error {
	if !c.opts.ValidateOnLoad {
		return nil
	}

	// JSON Schema path (G01): hard fail on violation.
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
