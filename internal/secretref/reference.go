// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package secretref is the single authority for Confii's secret-reference
// grammar. The public entry points live in the secret package; this package
// exists because the root confii package also needs the grammar and cannot
// import secret, which imports it.
package secretref

import (
	"errors"
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
// A component may not contain ':' or '}'. There is no escape mechanism, so a
// key containing a delimiter is not representable; the parser rejects such
// input rather than truncating it silently.
//
// The character class is deliberately identical to the two expressions this
// package replaced. Tightening it — excluding '{', say — changes how a
// nested value such as ${secret:${secret:k}} matches and breaks the
// resolver's idempotence, which the resolver fuzz target checks.
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
	`\$\{secret(?:@([A-Za-z0-9][A-Za-z0-9_.-]*))?:([^}:]+)(?::([^}:]*))?(?::([^}:]+))?\}`)

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

// ErrUnrepresentable reports a Reference whose components cannot be written in
// the grammar: an empty key, a component holding a ':' or '}' delimiter, or a
// provider alias that is not a valid alias.
//
// Such a Reference is reachable because Reference has exported fields and can
// be built by hand as well as by [Parse]. It is a programming error rather
// than a bad input, so it surfaces at serialization rather than at use.
var ErrUnrepresentable = errors.New("secret reference is not representable")

// providerPattern is the alias grammar, anchored. It must stay identical to
// the provider group inside pattern; the test that serializes and re-parses
// every generated Reference is what holds the two in step.
var providerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Validate reports whether the Reference can be written in the grammar, and so
// whether [Reference.String] can produce a form that parses back to it.
//
// Every Reference returned by [Parse], [Find], or [FromMatch] is valid by
// construction; only a hand-built one can fail. The error names the component
// at fault but never quotes it: a component is caller-controlled text, and
// echoing it into a message that may itself be embedded elsewhere is how the
// value would escape the check this method exists to make.
func (r Reference) Validate() error {
	if r.Provider != "" && !providerPattern.MatchString(r.Provider) {
		return fmt.Errorf("%w: provider must start with a letter or digit "+
			"and then hold only letters, digits, '_', '.', and '-'", ErrUnrepresentable)
	}
	if r.Key == "" {
		return fmt.Errorf("%w: key must not be empty", ErrUnrepresentable)
	}
	for _, c := range []struct {
		name  string
		value string
	}{{"key", r.Key}, {"field", r.Field}, {"version", r.Version}} {
		if strings.ContainsAny(c.value, ":}") {
			return fmt.Errorf("%w: %s must not contain ':' or '}'", ErrUnrepresentable, c.name)
		}
	}
	return nil
}

// String returns the canonical serialization of a valid Reference, which
// re-parses to an equal Reference.
//
// Serialization is canonical rather than merely valid: the shortest form that
// preserves every populated component is chosen, so a field is omitted when
// empty unless a version requires the placeholder separator.
//
// A Reference that [Reference.Validate] rejects has no serialization, and
// String answers with a diagnostic in the style of the fmt package rather than
// with text. Writing the components out regardless would be worse than
// useless: Reference{Key: "key:segment"} would render as ${secret:key:segment},
// which is a well-formed reference to the "key" secret's "segment" field — a
// different secret than the one the fields named, resolvable by anything that
// read it back. String cannot return an error, so it returns something that
// cannot be mistaken for a reference; use [Reference.MarshalText] where the
// failure needs to be handled rather than seen.
func (r Reference) String() string {
	if err := r.Validate(); err != nil {
		// Only the fixed reason text, never a component, so the diagnostic
		// cannot itself come to spell a reference.
		return "%!secret(" + strings.TrimPrefix(err.Error(), ErrUnrepresentable.Error()+": ") + ")"
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

// MarshalText implements [encoding.TextMarshaler], reporting the failure that
// [Reference.String] can only display. Use it wherever a Reference is written
// into JSON, YAML, or any other encoding that must not carry a diagnostic in
// place of a reference.
func (r Reference) MarshalText() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return []byte(r.String()), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler] with the same strictness
// as [Parse]: the text must be one reference and nothing else.
func (r *Reference) UnmarshalText(text []byte) error {
	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}
