package dictutil

// V-07 (Wave 24) — Adversarial tests for type-aware key normalization.
//
// Each test attempts to break the new collision-detection logic with
// inputs the pre-V-07 fmt.Sprint coercion silently mishandled. Tests
// also exercise the deeply-nested map[int]any vs map[string]any case
// the Adversarial Reviewer was specifically asked to attack.

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// V-07_a — bool true vs string "true" must collide loudly.
func TestNormalizeKeys_BoolStringCollision(t *testing.T) {
	in := map[any]any{
		true:   "enable_a",
		"true": "enable_b",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf("V-07: bool true / string \"true\" must collide, got nil error")
	}
	var ce *KeyCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("V-07: expected *KeyCollisionError, got %T (%v)", err, err)
	}
	if ce.Coerced != "true" {
		t.Fatalf("V-07: collision coerced key = %q, want %q", ce.Coerced, "true")
	}
}

// V-07_b — int 1 vs string "1" must collide loudly.
func TestNormalizeKeys_IntStringCollision(t *testing.T) {
	in := map[any]any{
		1:   "from_int",
		"1": "from_string",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf("V-07: int 1 / string \"1\" must collide, got nil error")
	}
	var ce *KeyCollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *KeyCollisionError, got %T", err)
	}
}

// V-07_c — float 1.0 vs int 1 collide because their canonical
// projections both yield "1". This is technically a *true* numeric
// collision rather than a type-confusion bug, but the routine still
// reports it loudly so the operator picks one representation.
func TestNormalizeKeys_FloatIntCollision(t *testing.T) {
	in := map[any]any{
		1:          "from_int",
		float64(1): "from_float",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf("V-07: int 1 / float 1.0 must collide, got nil error")
	}
}

// V-07_d — clean YAML with a single typed key normalizes successfully.
// The operator's intent is unambiguous; no error.
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

// V-07_e — Adversarial Reviewer's request: deeply nested
// map[int]any inside map[string]any. The recursive walker must descend
// into every value and surface a nested collision.
//
// The structure is:
//
//	map[string]any{
//	    "matrix": map[any]any{           // <-- wrapped as map[any]any (yaml.v3 shape)
//	        1: ...,
//	        "1": ...,                    // collides with int 1 at this level
//	    },
//	}
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
		t.Fatalf("V-07 nested: deep collision must propagate up, got nil error")
	}
	if !strings.Contains(err.Error(), "collide") {
		t.Fatalf("V-07 nested: expected collision message, got %v", err)
	}
}

// V-07_f — sequence with nested map[any]any element propagates a
// collision through the slice walker.
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
		t.Fatalf("V-07: collision inside []any must propagate, got nil")
	}
}

// V-07_g — non-comparable / unsupported map key types surface a typed
// *KeyCoercionError.
func TestNormalizeKeys_UnsupportedKeyType_TypedError(t *testing.T) {
	type customKey struct{ X int }
	// A struct used as a YAML key would actually fail at decode time —
	// but the dictutil layer must reject it loudly if it ever sees one.
	in := map[any]any{
		customKey{X: 1}: "v",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf("V-07: unsupported key type must surface error, got nil")
	}
	var ce *KeyCoercionError
	if !errors.As(err, &ce) {
		t.Fatalf("V-07: expected *KeyCoercionError, got %T (%v)", err, err)
	}
}

// V-07_h — float NaN keys are rejected (would corrupt Go map invariants).
func TestNormalizeKeys_NaNKey_Rejected(t *testing.T) {
	nan := math.NaN()
	in := map[any]any{
		nan: "v",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf("V-07: NaN key must be rejected, got nil")
	}
	var ce *KeyCoercionError
	if !errors.As(err, &ce) {
		t.Fatalf("V-07: expected *KeyCoercionError for NaN, got %T (%v)", err, err)
	}
}

// V-07_i — nil keys are rejected.
func TestNormalizeKeys_NilKey_Rejected(t *testing.T) {
	in := map[any]any{
		nil: "v",
	}
	_, err := NormalizeKeys(in)
	if err == nil {
		t.Fatalf("V-07: nil key must be rejected, got nil")
	}
}

// V-07_j — already-normalized map[string]any with no nested map[any]any
// is a no-op walk that never errors.
func TestNormalizeKeys_AlreadyNormalized_NoError(t *testing.T) {
	in := map[string]any{
		"a": 1,
		"b": map[string]any{"c": 2},
		"d": []any{1, 2, 3},
	}
	out, err := NormalizeKeys(in)
	if err != nil {
		t.Fatalf("V-07: pre-normalized map must not error, got %v", err)
	}
	if _, ok := out.(map[string]any); !ok {
		t.Fatalf("V-07: expected map[string]any output, got %T", out)
	}
}

// V-07_k — both first and second keys are reported in the error so the
// operator can pinpoint both source representations.
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
	// The message must reference both source representations.
	// Map iteration order is nondeterministic, so we just check both
	// type names appear in the report.
	if !strings.Contains(msg, "bool") || !strings.Contains(msg, "string") {
		t.Fatalf("V-07: collision error must name both bool and string types, got %q", msg)
	}
}
