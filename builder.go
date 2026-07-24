// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
)

// Builder provides a fluent API for constructing [Config] instances. Call
// [NewBuilder] to obtain a Builder, chain configuration methods, and finish
// with [Builder.Build] to produce a ready-to-use Config.
type Builder[T any] struct {
	opts []Option
}

// NewBuilder creates a new ConfigBuilder.
func NewBuilder[T any]() *Builder[T] {
	return &Builder[T]{}
}

// WithEnv sets the active environment.
func (b *Builder[T]) WithEnv(env string) *Builder[T] {
	b.opts = append(b.opts, WithEnv(env))
	return b
}

// AddLoader adds a single loader.
//
// Calling AddLoader marks the "loaders" option as explicitly set per the
// documented priority model (explicit code > self-config > built-in
// default). As a result, [applySelfConfig] will not append `default_files`
// loaders from a self-config file when the caller has staged at least one
// loader through the Builder. This preserves the invariant that explicit
// code wins over self-config (G05).
func (b *Builder[T]) AddLoader(l Loader) *Builder[T] {
	b.opts = append(b.opts, func(o *options) {
		o.Loaders = append(o.Loaders, l)
		o.explicitlySet["loaders"] = true
	})
	return b
}

// AddLoaders adds multiple loaders.
//
// Calling AddLoaders marks the "loaders" option as explicitly set per the
// documented priority model (explicit code > self-config > built-in
// default). As a result, [applySelfConfig] will not append `default_files`
// loaders from a self-config file when the caller has staged loaders
// through the Builder. This preserves the invariant that explicit code
// wins over self-config (G05).
//
// Calling AddLoaders with zero arguments is a no-op for the loader slice
// itself but still marks "loaders" as explicit, which is the desired
// behavior for callers that intentionally signal "I am supplying my own
// loader list, even if empty."
func (b *Builder[T]) AddLoaders(loaders ...Loader) *Builder[T] {
	b.opts = append(b.opts, func(o *options) {
		o.Loaders = append(o.Loaders, loaders...)
		o.explicitlySet["loaders"] = true
	})
	return b
}

// EnableDynamicReloading enables file watching.
func (b *Builder[T]) EnableDynamicReloading() *Builder[T] {
	b.opts = append(b.opts, WithDynamicReloading(true))
	return b
}

// DisableDynamicReloading disables file watching.
func (b *Builder[T]) DisableDynamicReloading() *Builder[T] {
	b.opts = append(b.opts, WithDynamicReloading(false))
	return b
}

// EnableEnvExpander enables ${VAR} expansion.
func (b *Builder[T]) EnableEnvExpander() *Builder[T] {
	b.opts = append(b.opts, WithEnvExpander(true))
	return b
}

// DisableEnvExpander disables ${VAR} expansion.
func (b *Builder[T]) DisableEnvExpander() *Builder[T] {
	b.opts = append(b.opts, WithEnvExpander(false))
	return b
}

// EnableTypeCasting enables automatic type casting.
func (b *Builder[T]) EnableTypeCasting() *Builder[T] {
	b.opts = append(b.opts, WithTypeCasting(true))
	return b
}

// DisableTypeCasting disables automatic type casting.
func (b *Builder[T]) DisableTypeCasting() *Builder[T] {
	b.opts = append(b.opts, WithTypeCasting(false))
	return b
}

// EnableDeepMerge enables deep merging.
func (b *Builder[T]) EnableDeepMerge() *Builder[T] {
	b.opts = append(b.opts, WithDeepMerge(true))
	return b
}

// DisableDeepMerge disables deep merging (shallow merge).
func (b *Builder[T]) DisableDeepMerge() *Builder[T] {
	b.opts = append(b.opts, WithDeepMerge(false))
	return b
}

// EnableDebug enables debug/source tracking mode.
func (b *Builder[T]) EnableDebug() *Builder[T] {
	b.opts = append(b.opts, WithDebugMode(true))
	return b
}

// WithSchemaValidation sets the validation schema and enables validate-on-load.
func (b *Builder[T]) WithSchemaValidation(schema any, strict bool) *Builder[T] {
	b.opts = append(b.opts, WithSchema(schema), WithValidateOnLoad(true), WithStrictValidation(strict))
	return b
}

// EnableFreezeOnLoad freezes config after loading.
func (b *Builder[T]) EnableFreezeOnLoad() *Builder[T] {
	b.opts = append(b.opts, WithFreezeOnLoad(true))
	return b
}

// Build creates the Config instance.
func (b *Builder[T]) Build(ctx context.Context) (*Config[T], error) {
	return New[T](ctx, b.opts...)
}
