// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package validate provides configuration validation implementations.
package validate

import (
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
)

// StructValidator validates configuration by decoding it into a typed struct
// using `confii` field-mapping tags and running `validate` struct-tag rules via
// go-playground/validator.
type StructValidator[T any] struct {
	validate *validator.Validate
}

// NewStructValidator creates a reusable validator for T. T should be a struct
// or pointer to a struct whose exported fields use optional `confii` mapping
// tags and `validate` constraint tags.
func NewStructValidator[T any]() *StructValidator[T] {
	return &StructValidator[T]{
		validate: validator.New(),
	}
}

// Validate decodes data into T using `confii` field-mapping tags and then runs
// `validate` rules. Decoding uses mapstructure weak scalar conversion; callers
// that require exact input types should validate those types separately.
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

// Decode decodes data into a newly allocated T. The `confii` tag binds keys
// that differ from Go field names, and embedded/nested structs follow
// mapstructure's standard decoding rules. Weak scalar conversion is enabled.
// Decode does not run `validate` tags; use [DecodeAndValidate] when validation
// is required.
func Decode[T any](data map[string]any) (*T, error) {
	var target T
	if err := decode(data, &target); err != nil {
		return nil, fmt.Errorf("struct decode: %w", err)
	}
	return &target, nil
}

// DecodeAndValidate combines [Decode] with go-playground/validator processing
// of `validate` tags. It returns no partially decoded value on error.
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
		TagName:          "confii",
		WeaklyTypedInput: true,
	}
	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}
	return decoder.Decode(data)
}
