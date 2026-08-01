// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"strings"

	"github.com/confiify/confii-go/v2/hook"
)

func buildValueResolverHook(opts options, baseDir string) hook.Func {
	resolvers := make(map[string]hook.ResolverFunc)
	const maxBytes int64 = 8 << 20

	if opts.UseStructuredResolver {
		resolvers["json"] = hook.NewJSONReferenceResolver(baseDir, maxBytes)
		resolvers["yaml"] = hook.NewYAMLReferenceResolver(baseDir, maxBytes)
	}
	if opts.UseFileResolver {
		resolvers["file"] = hook.NewFileReferenceResolver(baseDir, maxBytes)
	}
	if opts.UseURLResolver {
		resolvers["url"] = hook.NewURLReferenceResolver(nil, maxBytes)
	}
	if opts.UseCommandResolver {
		resolvers["cmd"] = hook.NewCommandReferenceResolver(0, maxBytes)
	}
	for scheme, resolver := range opts.valueResolvers {
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		if scheme != "" && resolver != nil {
			resolvers[scheme] = resolver
		}
	}
	if len(resolvers) == 0 {
		return nil
	}
	return hook.NewReferenceResolverHook(resolvers)
}
