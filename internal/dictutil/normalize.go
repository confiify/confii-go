// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package dictutil

import (
	"fmt"
	"math"
	"strconv"
)

// NormalizeKeys walks a value graph (typically the output of a YAML
// decoder) and returns an equivalent graph in which every map is
// map[string]any. gopkg.in/yaml.v3 emits map[interface{}]interface{}
// for any map containing a non-string key, which is incompatible with
// the rest of the library's data model (dot-path access, JSON export,
// typed configuration decoding all assume map[string]any).
//
// Keys are coerced via [coerceMapKey] using canonical per-type
// encodings: string passthrough, bool → "true" / "false", integer
// kinds via [strconv.FormatInt] / [strconv.FormatUint], float kinds
// via [strconv.FormatFloat] with the shortest round-trippable
// representation, NaN and nil rejected.
//
// When two distinct source keys produce the same coerced string —
// for example bool true and string "true" — NormalizeKeys returns a
// *[KeyCollisionError] naming both offending keys. The YAML loader
// and the in-package fileAutoLoader propagate this as a typed format
// error so the operator sees the exact ambiguity rather than a
// nondeterministic silent drop.
//
// Recursion covers three container shapes:
//
//   - map[any]any: rebuilt as map[string]any with coerced keys;
//     values recursed.
//   - map[string]any: values recursed for nested non-string-keyed
//     maps.
//   - []any: each element recursed.
//
// Scalars and unrecognized types are returned unchanged.
func NormalizeKeys(v any) (any, error) {
	switch m := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(m))
		// originals records the first source key seen for each coerced
		// string so a collision report names BOTH offending keys instead
		// of just the second one.
		originals := make(map[string]any, len(m))
		for k, val := range m {
			ck, err := coerceMapKey(k)
			if err != nil {
				return nil, err
			}
			if _, dup := out[ck]; dup {
				return nil, &KeyCollisionError{
					Coerced: ck,
					First:   originals[ck],
					Second:  k,
				}
			}
			normalized, err := NormalizeKeys(val)
			if err != nil {
				return nil, err
			}
			out[ck] = normalized
			originals[ck] = k
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			normalized, err := NormalizeKeys(val)
			if err != nil {
				return nil, err
			}
			out[k] = normalized
		}
		return out, nil
	case []any:
		out := make([]any, len(m))
		for i, val := range m {
			normalized, err := NormalizeKeys(val)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil
	default:
		return v, nil
	}
}

// coerceMapKey projects a YAML map key to its canonical string
// representation. NaN is rejected because it is not equal to itself
// and would break any later map lookup; nil is rejected because YAML
// null keys are unrepresentable in the library's data model. Other
// unsupported types return a typed [*KeyCoercionError].
func coerceMapKey(k any) (string, error) {
	switch v := k.(type) {
	case string:
		return v, nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.FormatInt(int64(v), 10), nil
	case int8:
		return strconv.FormatInt(int64(v), 10), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case uintptr:
		return strconv.FormatUint(uint64(v), 10), nil
	case float32:
		f := float64(v)
		if math.IsNaN(f) {
			return "", &KeyCoercionError{Type: fmt.Sprintf("%T", k), Reason: "NaN cannot be a map key"}
		}
		return strconv.FormatFloat(f, 'g', -1, 32), nil
	case float64:
		if math.IsNaN(v) {
			return "", &KeyCoercionError{Type: fmt.Sprintf("%T", k), Reason: "NaN cannot be a map key"}
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case nil:
		return "", &KeyCoercionError{Type: "nil", Reason: "null map key is not supported"}
	default:
		return "", &KeyCoercionError{Type: fmt.Sprintf("%T", k), Reason: "unsupported map key type"}
	}
}

// KeyCoercionError is returned by [NormalizeKeys] when a YAML map
// key cannot be projected to a canonical string. Type carries the
// offending Go type so the operator can pinpoint the ambiguity.
type KeyCoercionError struct {
	Type   string
	Reason string
}

// Error implements the error interface.
func (e *KeyCoercionError) Error() string {
	return fmt.Sprintf("dictutil: cannot coerce map key of type %s: %s", e.Type, e.Reason)
}

// KeyCollisionError is returned by [NormalizeKeys] when two distinct
// source keys project to the same canonical string. First and Second
// preserve both source values so the operator sees both
// representations that collided.
type KeyCollisionError struct {
	Coerced string
	First   any
	Second  any
}

// Error implements the error interface.
func (e *KeyCollisionError) Error() string {
	return fmt.Sprintf(
		"dictutil: distinct YAML map keys collide on coerced string %q (%T %v vs %T %v)",
		e.Coerced, e.First, e.First, e.Second, e.Second,
	)
}
