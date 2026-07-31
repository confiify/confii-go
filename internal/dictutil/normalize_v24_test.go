// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package dictutil

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestNormalizeKeys_BoolStringCollision(t *testing.T) {
	in := map[any]any{
		true:   "enable_a",
		"true": "enable_b",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(":bool true / string \"true\" must collide, got nil error")
	}
	var ce *KeyCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf(":expected *KeyCollisionError, got %T (%v)", err, err)
	}
	if ce.Coerced != "true" {
		t.Fatalf(":collision coerced key = %q, want %q", ce.Coerced, "true")
	}
}

func TestNormalizeKeys_IntStringCollision(t *testing.T) {
	in := map[any]any{
		1:   "from_int",
		"1": "from_string",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(":int 1 / string \"1\" must collide, got nil error")
	}
	var ce *KeyCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *KeyCollisionError, got %T", err)
	}
}

func TestNormalizeKeys_FloatIntCollision(t *testing.T) {
	in := map[any]any{
		1:          "from_int",
		float64(1): "from_float",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(":int 1 / float 1.0 must collide, got nil error")
	}
}

func TestNormalizeKeys_BoolKeyAlone_NormalizesToString(t *testing.T) {
	in := map[any]any{
		true: "enabled",
	}
	out, err := NormalizeKeys(in)
	if err != nil {
		t.Fatalf("clean bool key normalization failed: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if m["true"] != "enabled" {
		t.Fatalf("expected m[true] = enabled, got %v", m["true"])
	}
}

func TestNormalizeKeys_DeeplyNestedCollision_PropagatedUpward(t *testing.T) {
	in := map[string]any{
		"outer": map[string]any{
			"matrix": map[any]any{
				1:   "from_int",
				"1": "from_string",
			},
		},
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(" nested:deep collision must propagate up, got nil error")
	}
	if !strings.Contains(err.Error(), "collide") {
		t.Fatalf(" nested:expected collision message, got %v", err)
	}
}

func TestNormalizeKeys_SliceElementCollision_Propagated(t *testing.T) {
	in := []any{
		"first",
		map[any]any{
			true:   "a",
			"true": "b",
		},
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(":collision inside []any must propagate, got nil")
	}
}

func TestNormalizeKeys_UnsupportedKeyType_TypedError(t *testing.T) {
	type customKey struct{ X int }

	in := map[any]any{
		customKey{X: 1}: "v",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(":unsupported key type must surface error, got nil")
	}
	var ce *KeyCoercionError
	if !errors.As(err, &ce) {
		t.Fatalf(":expected *KeyCoercionError, got %T (%v)", err, err)
	}
}

func TestNormalizeKeys_NaNKey_Rejected(t *testing.T) {
	nan := math.NaN()
	in := map[any]any{
		nan: "v",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(":NaN key must be rejected, got nil")
	}
	var ce *KeyCoercionError
	if !errors.As(err, &ce) {
		t.Fatalf(":expected *KeyCoercionError for NaN, got %T (%v)", err, err)
	}
}

func TestNormalizeKeys_NilKey_Rejected(t *testing.T) {
	in := map[any]any{
		nil: "v",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf(":nil key must be rejected, got nil")
	}
}

func TestNormalizeKeys_AlreadyNormalized_NoError(t *testing.T) {
	in := map[string]any{
		"a": 1,
		"b": map[string]any{"c": 2},
		"d": []any{1, 2, 3},
	}
	out, err := NormalizeKeys(in)
	if err != nil {
		t.Fatalf(":pre-normalized map must not error, got %v", err)
	}
	if _, ok := out.(map[string]any); !ok {
		t.Fatalf(":expected map[string]any output, got %T", out)
	}
}

func TestNormalizeKeys_CollisionError_NamesBothKeys(t *testing.T) {
	in := map[any]any{
		true:   "a",
		"true": "b",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf("expected collision error")
	}
	msg := err.Error()

	if !strings.Contains(msg, "bool") || !strings.Contains(msg, "string") {
		t.Fatalf(":collision error must name both bool and string types, got %q", msg)
	}
}
