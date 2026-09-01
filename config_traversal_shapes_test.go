// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
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

// The typed admission passes walk the configuration too, and they walk it with
// a different function than redaction does: one substitutes a sentinel for
// every secret-bearing leaf so unknown keys can be judged without resolving,
// the other strips those leaves so remaining types can be judged. Both run only
// for a typed configuration with validation on load, which is why the untyped
// tests above never reach them.

type shapedTyped struct {
	Hosts    namedHosts     `mapstructure:"hosts"`
	Settings namedSettings  `mapstructure:"settings"`
	Pair     [2]string      `mapstructure:"pair"`
	Port     int            `mapstructure:"port"`
	Nested   map[string]any `mapstructure:"nested"`
}

func typedShapes() map[string]any {
	secret := "${secret:db/password}"
	return map[string]any{
		"hosts":    namedHosts{secret, "plain-host"},
		"settings": namedSettings{"password": secret},
		"pair":     [2]string{secret, "plain"},
		// A field typed int holding a reference carries a string until
		// resolution; judging it now would reject a valid configuration.
		"port":   "${secret:db/port}",
		"nested": map[string]any{"list": []any{secret}},
	}
}

// plausibleResolver answers each key with a value of the shape that key's
// field expects. canaryResolver answers every reference with one non-numeric
// string, which makes an int field fail *after* resolution and hides whether
// admission judged it *before* — the question this test exists to ask.
type plausibleResolver struct{}

func (plausibleResolver) Hook() hook.Func {
	return func(_ context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok || !strings.Contains(s, "${secret:") {
			return value, nil
		}
		if strings.Contains(s, "db/port") {
			return "5432", nil
		}
		return canary, nil
	}
}

func (plausibleResolver) ClearCache() {}

func TestTypedAdmission_WalksUnusualShapesWithoutRejectingValidConfiguration(t *testing.T) {
	cfg, err := confii.NewWithContext[shapedTyped](context.Background(),
		confii.WithLoaders(&g08Loader{source: "typed.yaml", data: typedShapes()}),
		confii.WithSecretResolver(plausibleResolver{}),
		confii.WithValidateOnLoad(true),
		confii.WithRejectUnknownKeys(true),
	)
	require.NoError(t, err,
		"a reference inside a named collection, an array, or an int-typed field "+
			"must not be judged as the wrong type before it is resolved")
	require.NotNil(t, cfg)
}

// The same walk must still catch a key the model does not declare, even when
// the surrounding configuration is full of shapes it has to traverse to get
// there.
func TestTypedAdmission_StillRejectsAnUndeclaredKeyAmongUnusualShapes(t *testing.T) {
	data := typedShapes()
	data["typoed"] = "value"

	_, err := confii.NewWithContext[shapedTyped](context.Background(),
		confii.WithLoaders(&g08Loader{source: "typed.yaml", data: data}),
		confii.WithSecretResolver(plausibleResolver{}),
		confii.WithValidateOnLoad(true),
		confii.WithRejectUnknownKeys(true),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typoed",
		"the undeclared key must be named")
}

// And a genuine type error is still a type error: a field typed int holding a
// value that is not a reference has a knowable type, so it is judged.
func TestTypedAdmission_StillRejectsAKnowableTypeError(t *testing.T) {
	data := typedShapes()
	data["port"] = "not-a-number"

	_, err := confii.NewWithContext[shapedTyped](context.Background(),
		confii.WithLoaders(&g08Loader{source: "typed.yaml", data: data}),
		confii.WithSecretResolver(plausibleResolver{}),
		confii.WithValidateOnLoad(true),
	)
	require.Error(t, err)
}
