// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
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
	data, err := c.ToDictWithContext(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
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
// The sensitivity check comes first, so a sensitive path holding a collection
// is replaced wholesale rather than descended into. A slice whose elements
// share its path cannot distinguish a secret element from a plain one, so
// redacting the whole slice is the only safe reading.
func redactValue(path string, value any, paths map[string]struct{}) any {
	if path != "" && valuePathIsSensitive(path, paths) {
		return redactedSecretValue
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			out[key] = redactValue(childPath, child, paths)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, child := range typed {
			// Same path, matching the classifier.
			out[index] = redactValue(path, child, paths)
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
