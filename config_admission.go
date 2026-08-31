// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"
	"regexp"

	"github.com/confiify/confii-go/v2/internal/secretref"
)

// secretCandidatePattern matches anything shaped like a secret reference,
// including forms the grammar rejects.
//
// The grammar itself only matches well-formed references, so it cannot be used
// to find a malformed one: ${secret:} simply does not match and would be
// carried into the configuration as a literal string. This deliberately looser
// expression finds the candidates, and each is then required to parse.
var secretCandidatePattern = regexp.MustCompile(`\$\{secret[^}]*\}`)

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
	return admitSecretReferences("", config)
}

// admitSecretReferences requires every reference-shaped token to parse.
//
// Errors carry the locator and the path it sits at, both of which describe
// where a secret lives rather than what it holds, and never a resolved value:
// nothing has been resolved at this point.
func admitSecretReferences(prefix string, config map[string]any) error {
	for key, value := range config {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		switch typed := value.(type) {
		case map[string]any:
			if err := admitSecretReferences(path, typed); err != nil {
				return err
			}
		case []any:
			for index, item := range typed {
				nested, ok := item.(map[string]any)
				if !ok {
					if err := admitSecretReferenceString(fmt.Sprintf("%s[%d]", path, index), item); err != nil {
						return err
					}
					continue
				}
				if err := admitSecretReferences(fmt.Sprintf("%s[%d]", path, index), nested); err != nil {
					return err
				}
			}
		default:
			if err := admitSecretReferenceString(path, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func admitSecretReferenceString(path string, value any) error {
	str, ok := value.(string)
	if !ok {
		return nil
	}
	for _, candidate := range secretCandidatePattern.FindAllString(str, -1) {
		if _, err := secretref.Parse(candidate); err != nil {
			return &ConfigError{
				Op:   "Admit",
				Code: ConfigErrorCodeValidation,
				Err: fmt.Errorf("%s: %q is not a valid secret reference: %w",
					path, candidate, err),
				Context: map[string]any{
					"path":      path,
					"reference": candidate,
				},
			}
		}
	}
	return nil
}
