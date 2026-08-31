// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import "github.com/confiify/confii-go/v2/internal/secretref"

// Reference is a parsed secret placeholder: an optional provider alias, a key,
// an optional structured field, and an optional version.
//
// # Grammar
//
//	${secret:key}                              default provider
//	${secret:key:field}                        with a structured field
//	${secret:key:field:version}                with an explicit version
//	${secret:key::version}                     version without a field
//	${secret@provider:key[:field][:version]}   routed to a named provider
//
// A provider alias starts with a letter or digit and may then contain letters,
// digits, underscore, dot, and hyphen.
//
// # Escaping
//
// There is none. Components are delimited by ':' and terminated by '}', and
// none of ':', '{', or '}' may appear inside a component. A key containing a
// delimiter is therefore not representable, and [ParseReference] rejects such
// input rather than truncating it. Store such a secret under a key that avoids
// the delimiters.
//
// # Compatibility
//
// This grammar is part of Confii's public interface. Within a major version,
// a string that parses today will continue to parse to an equal Reference, and
// [Reference.String] will keep producing a form that re-parses equally. New
// optional components may be added in positions that cannot change the meaning
// of an existing reference. Any change that would alter how an existing
// reference parses is a breaking change.
type Reference = secretref.Reference

// ReferenceError reports a locator that could not be parsed. Its Input holds
// the offending locator only: a locator names where a secret lives, never what
// it holds, so it is safe to log. A resolved value is never placed in an error.
type ReferenceError = secretref.Error

// ParseReference reads exactly one reference occupying the whole of s.
//
// Parsing is strict and purely syntactic. Leading or trailing text is an
// error, so a value that merely embeds a reference does not parse; use
// [FindReferences] for that. No provider is contacted and no registry is
// consulted, so a reference naming an unknown provider parses successfully —
// routing is resolved later, when the configuration materializes.
func ParseReference(s string) (Reference, error) { return secretref.Parse(s) }

// ContainsReference reports whether s holds at least one secret reference.
func ContainsReference(s string) bool { return secretref.Contains(s) }

// FindReferences returns every reference embedded in s, in order of
// appearance. It is the counterpart to [ParseReference] for values that mix
// references with other text, such as a connection string.
func FindReferences(s string) []Reference { return secretref.Find(s) }
