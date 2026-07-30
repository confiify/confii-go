// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"

	"github.com/confiify/confii-go/v2/hook"
)

// Builder accumulates constructor options for a [Config]. Builder methods
// mutate and return the same Builder for fluent chaining; a Builder is not safe
// for concurrent mutation. Build and BuildWithContext may be called repeatedly
// to create independent Config instances from the accumulated options.
type Builder[T any] struct {
	opts []Option
}

// NewBuilder returns an empty Builder that uses Confii's defaults and any
// self-configuration discovered when Build is called.
func NewBuilder[T any]() *Builder[T] {
	return &Builder[T]{}
}

// WithEnv selects the active environment. It has the same precedence and
// empty-string semantics as the [WithEnv] constructor option.
func (b *Builder[T]) WithEnv(env string) *Builder[T] {
	b.opts = append(b.opts, WithEnv(env))
	return b
}

// AddLoader appends one loader to the ordered source list. Later loaders have
// higher precedence under the selected merge strategy.
//
// Calling AddLoader makes the builder's loader list authoritative, so
// declarative sources from self-configuration are not substituted for it.
func (b *Builder[T]) AddLoader(l Loader) *Builder[T] {
	b.opts = append(b.opts, func(o *options) {
		o.Loaders = append(o.Loaders, l)
		o.explicitlySet["loaders"] = true
	})
	return b
}

// AddLoaders appends loaders in argument order. Later loaders have higher
// precedence under the selected merge strategy.
//
// Calling AddLoaders makes the builder's loader list authoritative, even when
// no loaders are supplied. This allows callers to intentionally disable
// declarative sources from self-configuration.
func (b *Builder[T]) AddLoaders(loaders ...Loader) *Builder[T] {
	b.opts = append(b.opts, func(o *options) {
		o.Loaders = append(o.Loaders, loaders...)
		o.explicitlySet["loaders"] = true
	})
	return b
}

// EnableDynamicReloading starts file watching after a successful build. It
// cannot be combined with EnableFreezeOnLoad.
func (b *Builder[T]) EnableDynamicReloading() *Builder[T] {
	b.opts = append(b.opts, WithDynamicReloading(true))
	return b
}

// DisableDynamicReloading disables watcher-driven reloads.
func (b *Builder[T]) DisableDynamicReloading() *Builder[T] {
	b.opts = append(b.opts, WithDynamicReloading(false))
	return b
}

// EnableEnvExpander enables ${VAR} expansion while snapshots are materialized.
func (b *Builder[T]) EnableEnvExpander() *Builder[T] {
	b.opts = append(b.opts, WithEnvExpander(true))
	return b
}

// DisableEnvExpander preserves ${VAR} text as ordinary string content.
func (b *Builder[T]) DisableEnvExpander() *Builder[T] {
	b.opts = append(b.opts, WithEnvExpander(false))
	return b
}

// EnableTypeCasting enables conversion of canonical string booleans and
// numbers while snapshots are materialized.
func (b *Builder[T]) EnableTypeCasting() *Builder[T] {
	b.opts = append(b.opts, WithTypeCasting(true))
	return b
}

// DisableTypeCasting preserves loaded string values as strings.
func (b *Builder[T]) DisableTypeCasting() *Builder[T] {
	b.opts = append(b.opts, WithTypeCasting(false))
	return b
}

// WithMergeStrategy selects the default strategy used to combine ordered
// source layers.
func (b *Builder[T]) WithMergeStrategy(strategy MergeStrategy) *Builder[T] {
	b.opts = append(b.opts, WithMergeStrategy(strategy))
	return b
}

// WithKeyHook adds a transformation for one exact dot-separated key. Hook
// errors fail construction or mutation without publishing a partial snapshot.
func (b *Builder[T]) WithKeyHook(key string, h hook.Func) *Builder[T] {
	b.opts = append(b.opts, WithKeyHook(key, h))
	return b
}

// WithValueHook adds a transformation selected when the candidate value deeply
// equals value at the value-hook stage.
func (b *Builder[T]) WithValueHook(value any, h hook.Func) *Builder[T] {
	b.opts = append(b.opts, WithValueHook(value, h))
	return b
}

// WithConditionHook adds a transformation selected by condition. An error from
// either the condition or hook aborts snapshot materialization.
func (b *Builder[T]) WithConditionHook(condition hook.Condition, h hook.Func) *Builder[T] {
	b.opts = append(b.opts, WithConditionHook(condition, h))
	return b
}

// WithGlobalHook adds a transformation for every leaf value in the snapshot.
func (b *Builder[T]) WithGlobalHook(h hook.Func) *Builder[T] {
	b.opts = append(b.opts, WithGlobalHook(h))
	return b
}

// EnableDebug records complete override histories in addition to ordinary
// source attribution. It may increase memory use for frequently overridden
// configurations.
func (b *Builder[T]) EnableDebug() *Builder[T] {
	b.opts = append(b.opts, WithDebugMode(true))
	return b
}

// WithSchemaValidation configures a struct or JSON Schema, enables validation
// during construction and subsequent materialization, and selects whether a
// violation is returned (strict) or logged (non-strict).
func (b *Builder[T]) WithSchemaValidation(schema any, strict bool) *Builder[T] {
	b.opts = append(b.opts, WithSchema(schema), WithValidateOnLoad(true), WithStrictValidation(strict))
	return b
}

// EnableFreezeOnLoad publishes the initial snapshot as immutable. Runtime
// mutation, extension, reload, and rollback methods then return
// [ErrConfigFrozen].
func (b *Builder[T]) EnableFreezeOnLoad() *Builder[T] {
	b.opts = append(b.opts, WithFreezeOnLoad(true))
	return b
}

// Build constructs an independent Config using Confii's implicit startup
// context and configured timeout. It returns only after the initial snapshot is
// fully materialized and validated.
func (b *Builder[T]) Build() (*Config[T], error) {
	return New[T](b.opts...)
}

// BuildWithContext constructs an independent Config using ctx. A nil context is
// rejected. Confii adds its configured fallback timeout only when ctx has no
// deadline; cancellation and context values propagate through initialization.
func (b *Builder[T]) BuildWithContext(ctx context.Context) (*Config[T], error) {
	return NewWithContext[T](ctx, b.opts...)
}
