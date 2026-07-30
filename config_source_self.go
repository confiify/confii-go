// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// SelfConfigSourceProviderFactory builds a Loader from one declarative
// `.confii.*` sources entry. Optional integrations register factories from
// their own modules so the core module does not acquire provider SDKs.
//
// The context is the same context passed to New. Provider constructors should
// use it for credential discovery and other potentially blocking setup.
type SelfConfigSourceProviderFactory func(context.Context, map[string]any) (Loader, error)

var selfConfigSourceProviders sync.Map // map[string]SelfConfigSourceProviderFactory

// RegisterSelfConfigSourceProvider registers an optional declarative source
// type. Names are case-insensitive. It panics for an empty name, a nil factory,
// or a duplicate normalized name, matching database/sql-style driver
// registration and preventing import order from silently replacing a provider.
func RegisterSelfConfigSourceProvider(name string, factory SelfConfigSourceProviderFactory) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		panic("confii: RegisterSelfConfigSourceProvider called with empty name")
	}
	if factory == nil {
		panic("confii: RegisterSelfConfigSourceProvider called with nil factory for " + name)
	}
	if _, loaded := selfConfigSourceProviders.LoadOrStore(name, factory); loaded {
		panic("confii: RegisterSelfConfigSourceProvider called twice for " + name)
	}
}

// LookupSelfConfigSourceProvider returns the optional source factory
// registered under name.
func LookupSelfConfigSourceProvider(name string) (SelfConfigSourceProviderFactory, bool) {
	value, ok := selfConfigSourceProviders.Load(strings.ToLower(strings.TrimSpace(name)))
	if !ok {
		return nil, false
	}
	return value.(SelfConfigSourceProviderFactory), true
}

func buildRegisteredSelfConfigSource(ctx context.Context, sourceType string, cfg map[string]any) (Loader, error) {
	factory, ok := LookupSelfConfigSourceProvider(sourceType)
	if !ok {
		return nil, &ConfigError{
			Op: "ApplySelfConfig",
			Err: fmt.Errorf(
				"%w: unsupported self-config source type %q (built in: environment_files, yaml, json, toml, ini, dotenv, environment; registered: %s)",
				ErrConfigLoad, sourceType, registeredSelfConfigSourceProviderNames(),
			),
		}
	}
	loader, err := factory(ctx, cfg)
	if err != nil {
		return nil, &ConfigError{
			Op:  "ApplySelfConfig",
			Err: fmt.Errorf("%w: self-config source provider %q failed to build: %w", ErrConfigLoad, sourceType, err),
		}
	}
	if loader == nil {
		return nil, &ConfigError{
			Op:  "ApplySelfConfig",
			Err: fmt.Errorf("%w: self-config source provider %q returned a nil loader", ErrConfigLoad, sourceType),
		}
	}
	return loader, nil
}

func registeredSelfConfigSourceProviderNames() string {
	names := make([]string, 0)
	selfConfigSourceProviders.Range(func(key, _ any) bool {
		if name, ok := key.(string); ok {
			names = append(names, name)
		}
		return true
	})
	if len(names) == 0 {
		return "(none)"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
