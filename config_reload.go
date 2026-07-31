// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"

	"github.com/confiify/confii-go/v2/sourcetrack"
)

// ReloadOption is a functional option that configures the behavior of
// [Config.Reload], such as enabling dry-run mode or overriding validation.
type ReloadOption func(*reloadOpts)

type reloadOpts struct {
	validate    *bool
	dryRun      bool
	incremental bool
}

// WithReloadValidate overrides validate_on_load for this reload. When true,
// the freshly loaded configuration is decoded into T and validated; on
// failure the private candidate is discarded and a [ErrConfigValidation] is
// returned. When false, validation is skipped regardless of the
// Config's WithValidateOnLoad setting.
// Validation runs whether the reload was driven by the incremental gate or by
// an unconditional WithIncremental(false), so a
// validation failure can never leave a "partially observed but not
// re-validated" state in place.
func WithReloadValidate(v bool) ReloadOption {
	return func(o *reloadOpts) { o.validate = &v }
}

// WithDryRun loads and validates a private candidate, then discards it without
// applying changes. The Config's live state is unchanged on a successful dry-run.
// A dry run does not emit reload or change metrics and events.
func WithDryRun(v bool) ReloadOption {
	return func(o *reloadOpts) { o.dryRun = v }
}

// WithIncremental enables per-source change detection. When true (the
// default), unchanged local file layers are reused and only changed files
// are loaded again. Sources without a local fingerprint (HTTP, cloud,
// environment, and custom remote loaders) are refreshed on every call.
// The complete ordered layer set is always re-merged and validated before
// commit, so skipping I/O never skips correctness checks.
func WithIncremental(v bool) ReloadOption {
	return func(o *reloadOpts) { o.incremental = v }
}

// prepareReloadCandidate refreshes an isolated candidate. The caller owns
// transaction publication, observability, and retries; a failed candidate is
// discarded and therefore requires no rollback of its private state.
func (c *Config[T]) prepareReloadCandidate(ctx context.Context, opts ...ReloadOption) (sourceTransactionOutcome, error) {
	ro := reloadOpts{incremental: true}
	for _, o := range opts {
		o(&ro)
	}

	// Select changed trackable sources. Sources that cannot
	// expose a local content fingerprint (HTTP, cloud, environment, custom
	// loaders) are refreshed because skipping them would make remote changes
	// permanently invisible. Unchanged file layers are reused from the cache.
	var selectedSources map[string]bool
	if ro.incremental {
		selectedSources = make(map[string]bool)
		for _, l := range c.loaders {
			source := l.Source()
			if !c.fileTracker.IsTrackable(source) || c.fileTracker.HasChanged(source) {
				selectedSources[source] = true
			}
		}
		for i, dependencies := range c.loaderDependencies {
			for _, dependency := range dependencies {
				if c.fileTracker.HasChanged(dependency) {
					selectedSources[c.loaders[i].Source()] = true
					break
				}
			}
		}
		if len(selectedSources) == 0 {
			return sourceTransactionOutcome{}, nil
		}
	}

	// Rebuild source tracking from the selected layers. The candidate owns its
	// tracker, so clearing it cannot affect the published configuration.
	c.sourceTracker.Restore(sourcetrack.Snapshot{})
	if err := c.loadSelected(ctx, selectedSources); err != nil {
		return sourceTransactionOutcome{}, err
	}
	if err := c.materializeEffectiveConfig(ctx); err != nil {
		materializeErr := &ConfigError{
			Op:  "Reload",
			Err: fmt.Errorf("%w: materialize effective configuration: %w", ErrConfigLoad, err),
		}
		return sourceTransactionOutcome{}, materializeErr
	}

	// Validate the rebuilt candidate when requested.
	// Validation runs after selected layers are rebuilt. The incremental gate
	// short-circuits only when nothing changed.
	shouldValidate := c.opts.ValidateOnLoad
	if ro.validate != nil {
		shouldValidate = *ro.validate
	}
	if err := c.validateCandidate(c.envConfig, shouldValidate); err != nil {
		return sourceTransactionOutcome{}, err
	}

	if ro.dryRun {
		c.logger.Info("dry-run reload completed, changes not applied")
		return sourceTransactionOutcome{}, nil
	}
	return sourceTransactionOutcome{publish: true}, nil
}
