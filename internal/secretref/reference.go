// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package secretref is the single authority for Confii's secret-reference
// grammar. The public entry points live in the secret package; this package
// exists because the root confii package also needs the grammar and cannot
// import secret, which imports it.
package secretref

import (
	"fmt"
	"regexp"
	"strings"
)

// Reference is a parsed secret placeholder.
//
// The grammar is delimiter-based:
//
//	${secret:key}                              default provider
//	${secret:key:field}                        with a structured field
//	${secret:key:field:version}                with an explicit version
//	${secret:key::version}                     version without a field
//	${secret@provider:key[:field][:version]}   routed to a named provider
//
// A component may not contain ':', '}', or '{'. There is no escape mechanism,
// so a key containing a delimiter is not representable; the parser rejects
// such input rather than truncating it silently.
type Reference struct {
	// Provider is the alias a reference is routed to. Empty selects the
	// default provider for the active environment.
	Provider string
	// Key is the secret's key or path. It is always present in a valid
	// reference.
	Key string
	// Field selects one member of a structured secret. Empty reads the whole
	// value.
	Field string
	// Version pins a specific version. Empty reads the current one.
	Version string
}

// Error reports a reference that could not be parsed.
//
// Input holds the offending locator only. A locator names where a secret
// lives, never what it holds, so it is safe to log; a resolved value is never
// placed in an error.
type Error struct {
	Input  string
	Reason string
}

func (e *Error) Error() string {
	return fmt.Sprintf("secret reference %q: %s", e.Input, e.Reason)
}

// pattern matches one reference anywhere in a string. It is the only
// definition of the grammar in the codebase.
//
// Group order: provider, key, field, version. The field group accepts an empty
// match so ${secret:key::version} is recognized; the version group requires a
// character so a bare trailing colon is not read as a version request.
var pattern = regexp.MustCompile(
	`\$\{secret(?:@([A-Za-z0-9][A-Za-z0-9_.-]*))?:([^{}:]+)(?::([^{}:]*))?(?::([^{}:]+))?\}`)

// Pattern returns the compiled grammar for scanning a larger string. Callers
// must not mutate the returned value.
func Pattern() *regexp.Regexp { return pattern }

// FromMatch builds a Reference from a submatch slice produced by Pattern.
func FromMatch(groups []string) Reference {
	var ref Reference
	if len(groups) > 1 {
		ref.Provider = groups[1]
	}
	if len(groups) > 2 {
		ref.Key = groups[2]
	}
	if len(groups) > 3 {
		ref.Field = groups[3]
	}
	if len(groups) > 4 {
		ref.Version = groups[4]
	}
	return ref
}

// Parse reads exactly one reference occupying the whole of s.
//
// Parsing is strict and purely syntactic: trailing or leading text is an
// error, and no provider is contacted, so an alias that is not registered
// anywhere still parses. Routing is resolved later by the configuration.
func Parse(s string) (Reference, error) {
	loc := pattern.FindStringIndex(s)
	if loc == nil {
		return Reference{}, &Error{Input: s, Reason: "not a secret reference"}
	}
	if loc[0] != 0 || loc[1] != len(s) {
		return Reference{}, &Error{
			Input:  s,
			Reason: "a reference must occupy the whole value; found surrounding text",
		}
	}
	return FromMatch(pattern.FindStringSubmatch(s)), nil
}

// Contains reports whether s holds at least one reference. It is the cheap
// check used before scanning.
func Contains(s string) bool { return strings.Contains(s, "${secret") && pattern.MatchString(s) }

// Find returns every reference in s, in order of appearance.
func Find(s string) []Reference {
	matches := pattern.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	refs := make([]Reference, 0, len(matches))
	for _, groups := range matches {
		refs = append(refs, FromMatch(groups))
	}
	return refs
}

// String returns the canonical serialization, which re-parses to an equal
// Reference. A reference with no key has no canonical form and serializes to
// the empty string.
//
// Serialization is canonical rather than merely valid: the shortest form that
// preserves every populated component is chosen, so a field is omitted when
// empty unless a version requires the placeholder separator.
func (r Reference) String() string {
	if r.Key == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("${secret")
	if r.Provider != "" {
		b.WriteString("@")
		b.WriteString(r.Provider)
	}
	b.WriteString(":")
	b.WriteString(r.Key)
	switch {
	case r.Field != "" && r.Version != "":
		b.WriteString(":")
		b.WriteString(r.Field)
		b.WriteString(":")
		b.WriteString(r.Version)
	case r.Field != "":
		b.WriteString(":")
		b.WriteString(r.Field)
	case r.Version != "":
		// The empty field slot must still be written so the version lands in
		// the right position.
		b.WriteString("::")
		b.WriteString(r.Version)
	}
	b.WriteString("}")
	return b.String()
}
