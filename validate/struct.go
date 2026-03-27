// Package validate provides configuration validation implementations.
package validate

import (
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/mitchellh/mapstructure"
)

// StructValidator validates configuration by decoding it into a typed struct
// and running struct tag validation (via go-playground/validator).
type StructValidator[T any] struct {
	validate *validator.Validate
}

// NewStructValidator creates a new struct validator for type T.
func NewStructValidator[T any]() *StructValidator[T] {
	return &StructValidator[T]{
		validate: validator.New(),
	}
}

// Validate decodes the config map into T and runs struct-tag validation rules.
func (v *StructValidator[T]) Validate(data map[string]any) error {
	var target T
	if err := decode(data, &target); err != nil {
		return fmt.Errorf("struct decode: %w", err)
	}
	if err := v.validate.Struct(target); err != nil {
		return fmt.Errorf("struct validation: %w", err)
	}
	return nil
}

// Decode decodes a config map into a typed struct T using mapstructure and returns a pointer to the result.
func Decode[T any](data map[string]any) (*T, error) {
	var target T
	if err := decode(data, &target); err != nil {
		return nil, fmt.Errorf("struct decode: %w", err)
	}
	return &target, nil
}

// DecodeAndValidate decodes the config map into T and validates struct tags in a single step.
func DecodeAndValidate[T any](data map[string]any) (*T, error) {
	var target T
	if err := decode(data, &target); err != nil {
		return nil, fmt.Errorf("struct decode: %w", err)
	}
	v := validator.New()
	if err := v.Struct(target); err != nil {
		return nil, fmt.Errorf("struct validation: %w", err)
	}
	return &target, nil
}

func decode(data map[string]any, target any) error {
	config := &mapstructure.DecoderConfig{
		Result:           target,
		TagName:          "mapstructure",
		WeaklyTypedInput: true,
	}
	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}
	return decoder.Decode(data)
}
