// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"fmt"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Admission and redaction both walk configuration by reflect.Kind rather than
// by a type switch on map[string]any and []any. That decision exists because a
// narrow switch passed named types, interface-held collections, and pointers
// through untouched — a secret inside one was neither admitted nor redacted,
// and the bug shipped three times before the traversal was rewritten.
//
// These shapes are therefore the regression surface, and they are exercised
// here rather than left to the shapes a YAML file happens to produce.

type namedHosts []string
type namedSettings map[string]any

func shapedConfig(t *testing.T, data map[string]any, opts ...confii.Option) *confii.Config[any] {
	t.Helper()
	all := append([]confii.Option{
		confii.WithLoaders(&g08Loader{source: "shapes.yaml", data: data}),
		confii.WithSecretResolver(&canaryResolver{}),
	}, opts...)
	cfg, err := confii.NewWithContext[any](context.Background(), all...)
	require.NoError(t, err)
	return cfg
}

// unusualShapes is one configuration carrying every traversal branch at once:
// a nil, a pointer, an interface-held map, a named slice and a named map, a
// non-string-keyed map, a byte slice, and a fixed-size array.
func unusualShapes() map[string]any {
	secret := "${secret:db/password}"
	return map[string]any{
		"nothing":    nil,
		"pointer":    &secret,
		"interface":  any(map[string]any{"held": secret}),
		"namedSlice": namedHosts{secret},
		"namedMap":   namedSettings{"inside": secret},
		"intKeyed":   map[int]string{1: secret},
		"rawBytes":   []byte("not-a-secret-reference"),
		"array":      [2]string{secret, "plain"},
		"deep":       map[string]any{"list": []any{map[string]any{"leaf": secret}}},
		"plain":      "visible",
	}
}

func TestRedactedDict_TraversesEveryShapeWithoutLeaking(t *testing.T) {
	cfg := shapedConfig(t, unusualShapes())

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)

	assert.NotContains(t, fmt.Sprint(safe), canary,
		"a resolved secret survived redaction inside an unusual shape")
	assert.Equal(t, "visible", safe["plain"], "unrelated values must survive")

	// A byte slice is a leaf. Walking it would turn a credential blob into a
	// list of numbers and redact none of it; it is copied whole instead.
	assert.Equal(t, []byte("not-a-secret-reference"), safe["rawBytes"])
}

func TestExportRedacted_TraversesEveryShapeWithoutLeaking(t *testing.T) {
	cfg := shapedConfig(t, unusualShapes())

	out, err := cfg.ExportRedacted("json")
	require.NoError(t, err)
	assert.NotContains(t, string(out), canary)
}

// Admission walks the same shapes before any provider is contacted, so a
// reference hidden inside one must still be judged.
func TestAdmission_JudgesReferencesInsideEveryShape(t *testing.T) {
	cfg := shapedConfig(t, unusualShapes())
	_, err := cfg.RedactedDict()
	assert.NoError(t, err, "every shape must materialize")

	// A malformed reference inside a named collection must be refused rather
	// than carried through as a literal.
	_, err = confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(&g08Loader{source: "bad.yaml", data: map[string]any{
			"hosts": namedHosts{"${secret:}"},
		}}),
		confii.WithSecretResolver(&canaryResolver{}),
	)
	require.Error(t, err, "a malformed reference inside a named slice must be refused")
}

func TestRedactedDict_NilAndEmptyInputsAreSafe(t *testing.T) {
	cfg := shapedConfig(t, map[string]any{"nothing": nil, "empty": map[string]any{}})

	safe, err := cfg.RedactedDict()
	require.NoError(t, err)
	assert.Nil(t, safe["nothing"])
	assert.Equal(t, map[string]any{}, safe["empty"])
}

// A nil context is a caller error, not a reason to return a half-built map.
func TestRedactedDict_RejectsNilAndCanceledContexts(t *testing.T) {
	cfg := shapedConfig(t, unusualShapes())

	var nilCtx context.Context
	_, err := cfg.RedactedDictWithContext(nilCtx)
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigInvalid)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cfg.RedactedDictWithContext(canceled)
	assert.ErrorIs(t, err, context.Canceled)

	_, err = cfg.ExportRedactedWithContext(canceled, "json")
	assert.ErrorIs(t, err, context.Canceled)
}
