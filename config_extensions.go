// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"
	"reflect"
	"strings"

	configexport "github.com/confiify/confii-go/v2/export"
)

func buildExporterRegistry(custom []Exporter) (map[string]Exporter, error) {
	registry := map[string]Exporter{
		"json": &configexport.JSONExporter{},
		"yaml": &configexport.YAMLExporter{},
		"toml": &configexport.TOMLExporter{},
	}
	for index, exporter := range custom {
		if isNilExtension(exporter) {
			return nil, &ConfigError{
				Op:   "New",
				Code: ConfigErrorCodeInvalid,
				Err:  fmt.Errorf("exporter %d is nil", index),
			}
		}
		format, err := exporterFormat(exporter)
		if err != nil {
			return nil, &ConfigError{Op: "New", Code: ConfigErrorCodeInvalid, Err: fmt.Errorf("exporter %d: %w", index, err)}
		}
		if format == "" || format != strings.TrimSpace(format) || format != strings.ToLower(format) {
			return nil, &ConfigError{
				Op:   "New",
				Code: ConfigErrorCodeInvalid,
				Err: fmt.Errorf(
					"exporter %d returned non-canonical format %q; use a non-empty lowercase name without surrounding whitespace",
					index, format,
				),
			}
		}
		registry[format] = exporter
	}
	return registry, nil
}

func exporterFormat(exporter Exporter) (format string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Format panicked: %v", recovered)
		}
	}()
	return exporter.Format(), nil
}

func validateCustomValidators(validators []Validator) error {
	for index, validator := range validators {
		if isNilExtension(validator) {
			return &ConfigError{
				Op:   "New",
				Code: ConfigErrorCodeInvalid,
				Err:  fmt.Errorf("validator %d is nil", index),
			}
		}
	}
	return nil
}

func isNilExtension(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
