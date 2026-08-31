// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/confiify/confii-go/v2/internal/dictutil"
)

// RedactedDict returns the materialized configuration as a deep copy with every
// secret-backed and declared-sensitive value replaced by a redaction marker.
//
// It is the safe counterpart to [Config.ToDict], which returns resolved secret
// values verbatim. Prefer this for anything that leaves the process: a log
// line, a diagnostic bundle, an error report, a support dump.
//
// Redaction covers values whose path carries a secret reference and paths
// declared through [WithSensitivePaths]. A parent is not redacted merely
// because a descendant is sensitive, so unrelated siblings survive: a
// configuration with a secret at database.password still reports
// database.host.
//
// The returned map is independent of Config; mutating it changes nothing.
func (c *Config[T]) RedactedDict() (map[string]any, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.RedactedDictWithContext(ctx)
}

// RedactedDictWithContext is the context-aware form of [Config.RedactedDict].
func (c *Config[T]) RedactedDictWithContext(ctx context.Context) (map[string]any, error) {
	if ctx == nil {
		return nil, &ConfigError{Op: "RedactedDict", Code: ConfigErrorCodeInvalid, Err: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The snapshot and its classification are captured under one read lock.
	// Taking them separately lets a concurrent reload or extend publish between
	// the two, pairing one revision's values with another revision's idea of
	// which paths are sensitive — and a path that became secret-bearing in the
	// newer revision would then be redacted against the older classification,
	// which does not list it.
	c.mu.RLock()
	src := c.envConfig
	if src == nil {
		src = c.mergedConfig
	}
	data := dictutil.DeepCopy(src)
	paths := cloneSensitivePaths(c.sensitivePaths)
	c.mu.RUnlock()

	return redactSensitiveValues("", data, paths), nil
}

// ExportRedacted serializes the effective snapshot in format with every
// secret-backed and declared-sensitive value replaced by a redaction marker.
//
// It is the safe counterpart to [Config.Export], which serializes resolved
// secret values verbatim. The registered exporters are the same, so the result
// is well-formed output in the requested format; only the sensitive values
// differ.
//
// No output path is accepted. Writing a redacted document to disk is an
// ordinary file write the caller can perform, and omitting the parameter keeps
// this method from looking like a drop-in replacement for an unredacted export
// to the same location.
func (c *Config[T]) ExportRedacted(format string) ([]byte, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.ExportRedactedWithContext(ctx, format)
}

// ExportRedactedWithContext is the context-aware form of
// [Config.ExportRedacted].
func (c *Config[T]) ExportRedactedWithContext(ctx context.Context, format string) ([]byte, error) {
	data, err := c.RedactedDictWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.exportDict(format, data)
}

// redactSensitiveValues walks config and replaces every sensitive value with
// the redaction marker, returning a new map.
//
// The traversal must mirror the one that produced the sensitive paths, or the
// two disagree and a secret slips through. collectSecretReferenceKeys descends
// into a slice without adding an index, so every element of a slice shares the
// path of the slice itself, and this walk does the same. Recursing with an
// index here would build paths such as nested[0].pw that no classification ever
// records, and the value would be copied through unredacted.
func redactSensitiveValues(prefix string, config map[string]any, paths map[string]struct{}) map[string]any {
	redacted, _ := redactValue(prefix, config, paths).(map[string]any)
	return redacted
}

// redactValue redacts one value of any shape.
//
// The traversal is driven by reflect.Kind rather than by a type switch on
// map[string]any and []any. A configuration can hold named collection types —
// type hostSet map[string]int, or a []string a loader produced — and a type
// switch passes those through untouched, which leaked a secret from inside a
// named map. Judging shape by kind covers named types, arrays, pointers, and
// interface-held collections without enumerating them.
//
// The sensitivity check comes first, so a sensitive path holding a collection
// is replaced wholesale rather than descended into. Elements of a sequence
// share the path of the sequence itself, matching collectSecretReferenceKeys,
// so a sequence cannot distinguish a secret element from a plain one and
// redacting all of it is the only safe reading.
func redactValue(path string, value any, paths map[string]struct{}) any {
	if path != "" && valuePathIsSensitive(path, paths) {
		return redactedSecretValue
	}
	if value == nil {
		return nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return redactValue(path, rv.Elem().Interface(), paths)

	case reflect.Map:
		// Only string-keyed maps address a configuration path. Anything else
		// cannot be reached by a dotted path, so it is copied as a leaf.
		if rv.Type().Key().Kind() != reflect.String {
			return dictutil.DeepCopyValue(value)
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			out[key] = redactValue(childPath, iter.Value().Interface(), paths)
		}
		return out

	case reflect.Slice, reflect.Array:
		// A byte slice is data, not a sequence of configuration values.
		if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
			return dictutil.DeepCopyValue(value)
		}
		out := make([]any, rv.Len())
		for index := 0; index < rv.Len(); index++ {
			// Same path, matching the classifier.
			out[index] = redactValue(path, rv.Index(index).Interface(), paths)
		}
		return out

	default:
		return dictutil.DeepCopyValue(value)
	}
}

// valuePathIsSensitive reports whether the value at path must be redacted:
// the path itself is sensitive, or it sits beneath a sensitive path.
//
// Deliberately narrower than pathIsSensitive, which also reports true for an
// ancestor of a sensitive path. That is the right question for whether a
// subtree touches a secret and the wrong one here: redacting a parent because
// one child is sensitive would discard every unrelated sibling.
func valuePathIsSensitive(path string, paths map[string]struct{}) bool {
	for sensitive := range paths {
		if path == sensitive || strings.HasPrefix(path, sensitive+".") {
			return true
		}
	}
	return false
}
