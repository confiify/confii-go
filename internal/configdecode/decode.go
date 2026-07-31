// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package configdecode provides the canonical decoders for configuration
// documents consumed by loaders, HTTP sources, and composition includes.
package configdecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/BurntSushi/toml"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/internal/formatparse"
	"github.com/confiify/confii-go/v2/internal/typecoerce"
	"go.yaml.in/yaml/v3"
	"gopkg.in/ini.v1"
)

// Map decodes exactly one document in format into Confii's map[string]any
// representation. YAML keys are normalized recursively, INI default-section
// keys are promoted to the root, and every format requires a top-level mapping.
// Explicit formats reject conclusive cross-format input before decoding.
func Map(data []byte, format formatparse.Format) (map[string]any, error) {
	if err := formatparse.ValidateDeclaredContent(format, data); err != nil {
		return nil, err
	}
	switch format {
	case formatparse.FormatYAML:
		return yamlMap(data)
	case formatparse.FormatJSON:
		return jsonMap(data)
	case formatparse.FormatTOML:
		var result map[string]any
		if err := toml.Unmarshal(data, &result); err != nil {
			return nil, err
		}
		return result, nil
	case formatparse.FormatINI:
		return iniMap(data)
	default:
		return nil, fmt.Errorf("unsupported configuration format %q", format)
	}
}

// AutoMap attempts JSON and then YAML. It is intended only for sources whose
// format is genuinely unspecified, such as HTTP responses without a usable
// Content-Type or path extension. Declarative file sources must select a
// concrete format instead.
func AutoMap(data []byte) (map[string]any, error) {
	result, jsonErr := Map(data, formatparse.FormatJSON)
	if jsonErr == nil {
		return result, nil
	}
	result, yamlErr := Map(data, formatparse.FormatYAML)
	if yamlErr == nil {
		return result, nil
	}
	return nil, fmt.Errorf("JSON parse: %v; YAML parse: %w", jsonErr, yamlErr)
}

func yamlMap(data []byte) (map[string]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	if err := requireEOF(func(target any) error { return decoder.Decode(target) }); err != nil {
		return nil, fmt.Errorf("multiple YAML documents are not supported: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	normalized, err := dictutil.NormalizeKeys(raw)
	if err != nil {
		return nil, err
	}
	result, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected top-level mapping, got %T", raw)
	}
	return result, nil
}

func jsonMap(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	if err := requireEOF(func(target any) error { return decoder.Decode(target) }); err != nil {
		return nil, fmt.Errorf("multiple JSON documents are not supported: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	result, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected top-level mapping, got %T", raw)
	}
	return result, nil
}

func requireEOF(decode func(any) error) error {
	var trailing any
	err := decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errors.New("additional document")
	}
	return err
}

func iniMap(data []byte) (map[string]any, error) {
	cfg, err := ini.Load(data)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any)
	for _, section := range cfg.Sections() {
		name := section.Name()
		if name == ini.DefaultSection {
			for _, key := range section.Keys() {
				result[key.Name()] = typecoerce.ParseScalar(key.Value(), false)
			}
			continue
		}
		sectionMap := make(map[string]any)
		for _, key := range section.Keys() {
			sectionMap[key.Name()] = typecoerce.ParseScalar(key.Value(), false)
		}
		result[name] = sectionMap
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
