// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"log/slog"
)

// prepareExtendCandidate loads and composes one additional source into an
// isolated candidate. It reports whether the candidate should be published;
// ignored failures and empty loader results are successful no-ops.
func (c *Config[T]) prepareExtendCandidate(ctx context.Context, l Loader) (sourceTransactionOutcome, error) {
	// Load the new source.
	data, err := l.Load(ctx)
	if err != nil {
		if cancellation := operationCancellation(ctx, err); cancellation != nil {
			return sourceTransactionOutcome{}, cancellation
		}
		switch c.opts.OnError {
		case ErrorPolicyRaise:
			return sourceTransactionOutcome{}, err
		case ErrorPolicyWarn:
			c.logger.Warn(
				"loader error",
				slog.String("source", l.Source()),
				slog.String("error", err.Error()),
			)
			return sourceTransactionOutcome{}, nil
		case ErrorPolicyIgnore:
			return sourceTransactionOutcome{}, nil
		default:
			return sourceTransactionOutcome{}, err
		}
	}
	if data == nil {
		return sourceTransactionOutcome{}, nil
	}

	// Process _include, _defaults, and _merge_strategy directives. Composition
	// errors follow the configured error policy.
	composed, dependencies, err := c.composer.ComposeWithDependenciesWithContext(ctx, data, l.Source())
	if err != nil {
		if cancellation := operationCancellation(ctx, err); cancellation != nil {
			return sourceTransactionOutcome{}, cancellation
		}
		switch c.opts.OnError {
		case ErrorPolicyRaise:
			return sourceTransactionOutcome{}, err
		case ErrorPolicyWarn:
			c.logger.Warn(
				"composition error",
				slog.String("source", l.Source()),
				slog.String("error", err.Error()),
			)
			composed = data
			dependencies = nil
		case ErrorPolicyIgnore:
			composed = data
			dependencies = nil
		default:
			return sourceTransactionOutcome{}, err
		}
	}

	// Resolve default and active-environment sections using the same handler as
	// initial loading.
	resolved := c.envHandler.Resolve(composed, c.env)

	// Merge into candidate state. A later failure discards the candidate.
	c.mergedConfig = c.merger.Merge(c.mergedConfig, composed)
	rawBase := c.unresolvedEnvConfig
	if rawBase == nil {
		rawBase = c.envConfig
	}
	c.envConfig = c.merger.Merge(rawBase, resolved)
	if err := c.materializeEffectiveConfig(ctx); err != nil {
		materializeErr := &ConfigError{
			Op:  "Extend",
			Err: fmt.Errorf("%w: materialize effective configuration: %w", ErrConfigLoad, err),
		}
		return sourceTransactionOutcome{}, materializeErr
	}

	if err := c.validateMaterializedCandidate(c.envConfig); err != nil {
		return sourceTransactionOutcome{}, err
	}

	c.loaders = append(c.loaders, l)
	c.loaderLayers = append(c.loaderLayers, copyMap(composed))
	c.loaderDependencies = append(c.loaderDependencies, append([]string(nil), dependencies...))

	// Track the source with the file tracker so subsequent Reload calls
	// can detect changes to it. Non-file sources (HTTP, env, etc.)
	// produce a Stat error which we record only at debug level.
	if ferr := c.fileTracker.Track(l.Source()); ferr != nil {
		c.logger.Debug(
			"extend: source not tracked for incremental reload",
			slog.String("source", l.Source()),
			slog.String("reason", ferr.Error()),
		)
	}
	for _, dependency := range dependencies {
		c.trackCompositionDependency(dependency)
	}

	// Track the resolved overlay against this loader so Explain /
	// Layers / GetSourceInfo report this source for every key it
	// contributed (matching c.load's per-loader TrackConfig pattern).
	loaderType := loaderTypeName(l)
	c.sourceTracker.TrackConfig(resolved, l.Source(), loaderType, c.env, "")
	return sourceTransactionOutcome{publish: true}, nil
}

// trackCompositionDependency registers an included file for incremental
// reloads. Keeping the best-effort failure handling in a small helper makes
// the TOCTOU case (the include disappears after composition) deterministic to
// test without weakening Extend's transaction semantics.
func (c *Config[T]) trackCompositionDependency(dependency string) {
	if ferr := c.fileTracker.Track(dependency); ferr != nil {
		c.logger.Debug("extend: composition dependency not tracked", slog.String("source", dependency), slog.String("reason", ferr.Error()))
	}
}
