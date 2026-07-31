// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
)

// Reload refreshes configuration transactionally. All loader, composition,
// secret-provider, and validation work runs against a private candidate, so
// concurrent readers continue to observe the last complete snapshot. The
// candidate is published under a short write lock only after all processing
// succeeds.
func (c *Config[T]) Reload(opts ...ReloadOption) error {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.ReloadWithContext(ctx, opts...)
}

// ReloadWithContext is the context-aware form of [Config.Reload]. A nil or
// canceled context, a frozen or closed Config, loader/composition failures,
// hook or secret-provider failures, and validation failures are returned
// without publishing a partial snapshot. Concurrent mutations are reconciled
// by rebuilding the candidate until it can be committed or ctx ends.
//
// Reload options control incremental source selection and dry-run validation;
// see [WithIncremental], [WithDryRun], and [WithReloadValidate].
func (c *Config[T]) ReloadWithContext(ctx context.Context, opts ...ReloadOption) error {
	return c.runSourceTransaction(ctx, reloadTransaction, func(ctx context.Context, candidate *Config[T]) (sourceTransactionOutcome, error) {
		return candidate.prepareReloadCandidate(ctx, opts...)
	})
}
