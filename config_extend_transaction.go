// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
)

// Extend adds l as the highest-precedence source, then loads, composes,
// resolves, materializes, and validates a private candidate before publishing
// it atomically. The loader remains part of subsequent Reload operations.
// Passing nil, extending a frozen or closed Config, or any processing failure
// returns an error and preserves the previous snapshot.
func (c *Config[T]) Extend(l Loader) error {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.ExtendWithContext(ctx, l)
}

// ExtendWithContext is the context-aware form of [Config.Extend]. The context
// bounds loader, composition, hook, secret-provider, and validation work. A
// nil or canceled context returns an error without publishing a partial
// snapshot.
func (c *Config[T]) ExtendWithContext(ctx context.Context, l Loader) error {
	if ctx == nil {
		return &ConfigError{Op: "Extend", Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	if l == nil {
		return &ConfigError{Op: "Extend", Err: fmt.Errorf("%w: nil loader", ErrConfigInvalid)}
	}
	return c.runSourceTransaction(ctx, extendTransaction, func(ctx context.Context, candidate *Config[T]) (sourceTransactionOutcome, error) {
		return candidate.prepareExtendCandidate(ctx, l)
	})
}
