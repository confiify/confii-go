// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/confiify/confii-go/v2/internal/secretref"
	"github.com/confiify/confii-go/v2/validate"
)

// secretCandidatePattern matches anything shaped like a secret reference,
// including forms the grammar rejects.
//
// The grammar itself only matches well-formed references, so it cannot be used
// to find a malformed one: ${secret:} simply does not match and would be
// carried into the configuration as a literal string. This deliberately looser
// expression finds the candidates, and each is then required to parse.
var secretCandidatePattern = regexp.MustCompile(`\$\{secret[^}]*\}`)

// secretOpenerPattern matches the start of a reference regardless of whether it
// is ever closed, so an unterminated one can be reported rather than carried
// through as a literal string.
var secretOpenerPattern = regexp.MustCompile(`\$\{secret`)

// admitBeforeResolution runs the checks that can be made before any provider is
// contacted.
//
// # What can be admitted here, and what cannot
//
// A reference is a syntactic object, so its grammar can be checked without
// reaching a provider. That is what runs here, and it is the check whose
// absence was most expensive: a single mistyped placeholder previously cost a
// provider round trip for every other key in the file before anything noticed.
//
// Value-dependent admission cannot move here, and pretending otherwise would
// produce false rejections. A field typed int holding ${secret:db/port} carries
// a string until the secret is resolved; JSON Schema constraints and exact-type
// admission describe the resolved value, not the placeholder standing in for
// it. Those run after resolution, where the values they describe exist.
//
// Sensitivity classification also stays where it is, after resolution, and that
// is deliberate. It is derived from the unresolved configuration either way, so
// the classification is identical; what differs is when it is assigned. A
// failed reload rolls the configuration back to its previous state, and
// sensitivePaths is not part of that rollback, so assigning it from a candidate
// that never became live would leave the wrong classification behind.
//
// Provider routing is not admitted here either. When the caller supplies a
// resolver, routing belongs to it: a custom ManagedSecretResolver may carry its
// own registry, and refusing an alias Confii does not recognize would break it.
func (c *Config[T]) admitBeforeResolution(config map[string]any) error {
	if err := admitSecretReferences("", any(config)); err != nil {
		return err
	}
	return c.admitTypedShape(config)
}

// admitTypedShape rejects, before any provider is contacted, configurations
// whose shape the typed model does not accept.
//
// It runs two passes because the two questions need different inputs.
//
// The first asks which keys exist. Secret-bearing leaves are replaced by a
// sentinel rather than removed, so an undeclared key still presents itself even
// when its value is a secret reference; removing it, as an earlier revision
// did, hid exactly the violation being looked for and cost a provider round
// trip before post-resolution validation caught it. Weak typing is forced on so
// the sentinel cannot fail on a type, and only an unknown-key complaint is
// treated as a rejection — anything else may be an artifact of the sentinel and
// is left to the second pass and to post-resolution validation.
//
// The second asks whether the values that do exist have the right types. Here
// secret-bearing leaves are removed, because a field typed int holding
// ${secret:db/port} carries a string until resolution and judging it now would
// reject a valid configuration. Every remaining value is one whose type is
// knowable, so the caller's own casting policy applies unchanged.
//
// Both passes require WithValidateOnLoad, matching the contract that already
// existed: without it a shape problem is reported at the first typed read, and
// with it at construction. This moves the construction-time case ahead of the
// providers; it does not change which configurations are rejected.
func (c *Config[T]) admitTypedShape(config map[string]any) error {
	if !c.opts.ValidateOnLoad || !configTypeSupportsStructValidation[T]() {
		return nil
	}

	if c.opts.RejectUnknownKeys {
		sentinelled, ok := substituteSecretBearingLeaves(config, admissionSentinel).(map[string]any)
		if !ok {
			return nil
		}
		if _, err := validate.DecodeWithOptions[T](sentinelled, validate.Options{
			WeaklyTypedInput:  true,
			RejectUnknownKeys: true,
		}); err != nil && mentionsUnknownKeys(err) {
			return &ConfigError{
				Op:   "Admit",
				Code: ConfigErrorCodeValidation,
				Err:  fmt.Errorf("configuration declares keys the model does not: %w", err),
			}
		}
	}

	stripped, ok := withoutSecretBearingLeaves(config).(map[string]any)
	if !ok {
		return nil
	}
	if _, err := validate.DecodeWithOptions[T](stripped, validate.Options{
		WeaklyTypedInput:  c.opts.UseTypeCasting,
		RejectUnknownKeys: false,
	}); err != nil {
		return &ConfigError{
			Op:   "Admit",
			Code: ConfigErrorCodeValidation,
			Err:  fmt.Errorf("configuration values do not match the model: %w", err),
		}
	}
	return nil
}

// admissionSentinel stands in for a secret reference during the shape check. It
// converts cleanly under weak typing to a string, number or boolean, so a
// declared field carrying a secret does not fail on its type while its key is
// being checked.
const admissionSentinel = "0"

// mentionsUnknownKeys reports whether a decode failure is about undeclared keys
// rather than about values. mapstructure phrases that failure as "has invalid
// keys", which is the only signal it offers.
func mentionsUnknownKeys(err error) bool {
	return err != nil && strings.Contains(err.Error(), "invalid keys")
}

// substituteSecretBearingLeaves copies value with every string holding a secret
// reference replaced by replacement, keeping the key present so a structural
// check can still see it.
func substituteSecretBearingLeaves(value any, replacement string) any {
	return mapSecretBearingLeaves(value, func() (any, bool) { return replacement, true })
}

// withoutSecretBearingLeaves copies value with every string holding a secret
// reference removed, so a decode can judge types without meeting a placeholder
// where a typed value will eventually sit.
func withoutSecretBearingLeaves(value any) any {
	return mapSecretBearingLeaves(value, func() (any, bool) { return nil, false })
}

// mapSecretBearingLeaves copies value, calling replace for each string holding
// a secret reference. A replacement of (_, false) drops the entry entirely.
//
// The walk is kind-driven so named maps, named slices, arrays, pointers and
// interface-held collections are covered without enumerating them.
func mapSecretBearingLeaves(value any, replace func() (any, bool)) any {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return mapSecretBearingLeaves(rv.Elem().Interface(), replace)

	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return value
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			child := iter.Value().Interface()
			if str, isString := child.(string); isString && secretCandidatePattern.MatchString(str) {
				if replacement, keep := replace(); keep {
					out[iter.Key().String()] = replacement
				}
				continue
			}
			out[iter.Key().String()] = mapSecretBearingLeaves(child, replace)
		}
		return out

	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
			return value
		}
		out := make([]any, 0, rv.Len())
		for index := 0; index < rv.Len(); index++ {
			child := rv.Index(index).Interface()
			if str, isString := child.(string); isString && secretCandidatePattern.MatchString(str) {
				if replacement, keep := replace(); keep {
					out = append(out, replacement)
				}
				continue
			}
			out = append(out, mapSecretBearingLeaves(child, replace))
		}
		return out

	default:
		return value
	}
}

// admitSecretReferences requires every reference-shaped token to parse.
//
// Traversal is driven by reflect.Kind, not a type switch on map[string]any and
// []any. An earlier revision recursed into a slice only when the element was a
// map, so a reference nested one slice deeper was handed to the scalar checker
// and ignored; named collection types were invisible for the same reason.
//
// Errors carry the locator and the path it sits at, both of which describe
// where a secret lives rather than what it holds, and never a resolved value:
// nothing has been resolved at this point.
func admitSecretReferences(prefix string, value any) error {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return admitSecretReferences(prefix, rv.Elem().Interface())

	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil
		}
		iter := rv.MapRange()
		for iter.Next() {
			path := iter.Key().String()
			if prefix != "" {
				path = prefix + "." + iter.Key().String()
			}
			if err := admitSecretReferences(path, iter.Value().Interface()); err != nil {
				return err
			}
		}
		return nil

	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
			return nil
		}
		for index := 0; index < rv.Len(); index++ {
			if err := admitSecretReferences(prefix, rv.Index(index).Interface()); err != nil {
				return err
			}
		}
		return nil

	case reflect.String:
		return admitSecretReferenceString(prefix, rv.String())

	default:
		return nil
	}
}

// admitSecretReferenceString checks one string for reference-shaped tokens.
//
// Two failures are possible. A token that closes but does not parse is
// malformed. A token that never closes produces no candidate at all, so it must
// be detected by looking for an opener the candidate expression did not
// consume; without that check ${secret:unclosed passes through as a literal.
func admitSecretReferenceString(path, value string) error {
	for _, candidate := range secretCandidatePattern.FindAllString(value, -1) {
		if _, err := secretref.Parse(candidate); err != nil {
			return admissionError(path, candidate,
				fmt.Errorf("is not a valid secret reference: %w", err))
		}
	}

	// Any opener not consumed by a candidate above is unterminated.
	consumed := make(map[int]struct{})
	for _, loc := range secretCandidatePattern.FindAllStringIndex(value, -1) {
		consumed[loc[0]] = struct{}{}
	}
	for _, loc := range secretOpenerPattern.FindAllStringIndex(value, -1) {
		if _, ok := consumed[loc[0]]; ok {
			continue
		}
		return admissionError(path, value[loc[0]:],
			errors.New("is an unterminated secret reference: no closing brace"))
	}
	return nil
}

func admissionError(path, locator string, cause error) error {
	return &ConfigError{
		Op:   "Admit",
		Code: ConfigErrorCodeValidation,
		Err:  fmt.Errorf("%s: %q %w", path, locator, cause),
		Context: map[string]any{
			"path":      path,
			"reference": locator,
		},
	}
}
