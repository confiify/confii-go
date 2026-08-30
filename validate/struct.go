// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package validate provides configuration validation implementations.
package validate

import (
	"fmt"

	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
)

// Options controls how configuration data is decoded into a typed
// struct.
type Options struct {
	// WeaklyTypedInput enables mapstructure's weak scalar conversion,
	// which accepts a quoted "5432" for an int field and "yes" for a
	// bool. Disable it to require the input to already carry the
	// declared types; a mismatch then fails the decode instead of
	// being converted silently.
	WeaklyTypedInput bool
}

// defaultOptions preserves the historical decoding behavior for the
// functions that do not take an Options value.
func defaultOptions() Options {
	return Options{WeaklyTypedInput: true}
}

// StructValidator validates configuration by decoding it into a typed struct
// using `confii` field-mapping tags and running `validate` struct-tag rules via
// go-playground/validator.
type StructValidator[T any] struct {
	validate *validator.Validate
	opts     Options
}

// NewStructValidator creates a reusable validator for T. T should be a struct
// or pointer to a struct whose exported fields use optional `confii` mapping
// tags and `validate` constraint tags.
func NewStructValidator[T any]() *StructValidator[T] {
	return NewStructValidatorWithOptions[T](defaultOptions())
}

// NewStructValidatorWithOptions creates a reusable validator for T that
// decodes under opts. Use it with Options.WeaklyTypedInput set to false
// when the caller requires the input to carry exact types.
func NewStructValidatorWithOptions[T any](opts Options) *StructValidator[T] {
	return &StructValidator[T]{
		validate: validator.New(),
		opts:     opts,
	}
}

// Validate decodes data into T using `confii` field-mapping tags and then runs
// `validate` rules. Decoding uses mapstructure weak scalar conversion unless
// the validator was built by [NewStructValidatorWithOptions] with
// Options.WeaklyTypedInput disabled.
func (v *StructValidator[T]) Validate(data map[string]any) error {
	var target T
	if err := decodeWithOptions(data, &target, v.opts); err != nil {
		return fmt.Errorf("struct decode: %w", err)
	}
	if err := v.validate.Struct(target); err != nil {
		return fmt.Errorf("struct validation: %w", err)
	}
	return nil
}

// Decode decodes data into a newly allocated T. The `confii` tag binds keys
// that differ from Go field names, and embedded/nested structs follow
// mapstructure's standard decoding rules. Weak scalar conversion is enabled;
// use [DecodeWithOptions] to disable it. Decode does not run `validate` tags;
// use [DecodeAndValidate] when validation is required.
func Decode[T any](data map[string]any) (*T, error) {
	return DecodeWithOptions[T](data, defaultOptions())
}

// DecodeWithOptions decodes data into a newly allocated T under opts.
// With Options.WeaklyTypedInput disabled, a value whose type differs
// from the declared field type fails the decode rather than being
// converted.
func DecodeWithOptions[T any](data map[string]any, opts Options) (*T, error) {
	var target T
	if err := decodeWithOptions(data, &target, opts); err != nil {
		return nil, fmt.Errorf("struct decode: %w", err)
	}
	return &target, nil
}

// DecodeAndValidate combines [Decode] with go-playground/validator processing
// of `validate` tags. It returns no partially decoded value on error.
func DecodeAndValidate[T any](data map[string]any) (*T, error) {
	return DecodeAndValidateWithOptions[T](data, defaultOptions())
}

// DecodeAndValidateWithOptions combines [DecodeWithOptions] with
// go-playground/validator processing of `validate` tags. It returns no
// partially decoded value on error.
func DecodeAndValidateWithOptions[T any](data map[string]any, opts Options) (*T, error) {
	target, err := DecodeWithOptions[T](data, opts)
	if err != nil {
		return nil, err
	}
	v := validator.New()
	if err := v.Struct(*target); err != nil {
		return nil, fmt.Errorf("struct validation: %w", err)
	}
	return target, nil
}

func decodeWithOptions(data map[string]any, target any, opts Options) error {
	config := &mapstructure.DecoderConfig{
		Result:           target,
		TagName:          "confii",
		WeaklyTypedInput: opts.WeaklyTypedInput,
	}
	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}
	return decoder.Decode(data)
}
