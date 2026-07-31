// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"time"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/observe"
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
// failure the entire reload is rolled back and a [ErrConfigValidation]
// is returned. When false, validation is skipped regardless of the
// Config's WithValidateOnLoad setting.
// Validation runs whether the reload was driven by the incremental gate or by
// an unconditional WithIncremental(false), so a
// validation failure can never leave a "partially observed but not
// re-validated" state in place.
func WithReloadValidate(v bool) ReloadOption {
	return func(o *reloadOpts) { o.validate = &v }
}

// WithDryRun loads, validates, and then rolls back without applying
// changes. The Config's live state is unchanged on a successful dry-run.
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

// reloadCandidate refreshes an isolated configuration candidate. Loaders,
// composition, materialization, and validation must succeed before the caller
// can publish the candidate. Incremental reloads reuse unchanged file layers
// and refresh sources that cannot be tracked as files.
func (c *Config[T]) reloadCandidate(ctx context.Context, opts ...ReloadOption) error {
	c.mu.Lock()
	// Callback dispatch requires the lock to be released because callbacks may
	// invoke other Config methods.
	unlocked := false
	var failureEventErr error
	var failureEventDuration time.Duration
	var failureEmitter *observe.EventEmitter
	defer func() {
		if !unlocked {
			c.mu.Unlock()
		}
		if failureEventErr != nil && failureEmitter != nil {
			failureEmitter.Emit("reload_failed", failureEventErr, failureEventDuration)
		}
	}()

	if c.frozen {
		return NewFrozenError("Reload")
	}
	if c.closed {
		return NewClosedError("Reload")
	}

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
			return nil
		}
	}

	// Snapshot every component c.load mutates so the rollback
	// closure can restore them atomically. fileTracker participates
	// because c.load records mtime+sha256 of each loaded file; without
	// rolling it back, a subsequent incremental Reload would see the
	// recorded-bad hash and short-circuit the change-detection gate.
	oldEnv := copyMap(c.envConfig)
	oldUnresolvedEnv := copyMap(c.unresolvedEnvConfig)
	oldMerged := copyMap(c.mergedConfig)
	trackerSnap := c.sourceTracker.Snapshot()
	fileTrackerSnap := c.fileTracker.Snapshot()
	oldLayers := copyLoaderLayers(c.loaderLayers)
	oldDependencies := copyLoaderDependencies(c.loaderDependencies)
	start := time.Now()

	// rollback restores the complete transaction state before reporting the
	// failure. All load and validation exits use the same restoration path.
	rollback := func(failureErr error) {
		c.envConfig = oldEnv
		c.unresolvedEnvConfig = oldUnresolvedEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		c.fileTracker.Restore(fileTrackerSnap)
		c.loaderLayers = oldLayers
		c.loaderDependencies = oldDependencies
		// Failure-path metrics/events fire only after
		// rollback has restored the state, so any observer that probes
		// the Config from inside a "reload_failed" listener sees the
		// pre-reload values, not the doomed post-load ones.
		dur := time.Since(start)
		if c.observer != nil {
			c.observer.RecordReloadFailed(dur)
		}
		failureEventErr = failureErr
		failureEventDuration = dur
		failureEmitter = c.eventEmitter
	}

	// Rebuild source tracking from the selected layers.
	// Reset the source tracker before c.load runs so a successful reload starts
	// from an empty tracker and re-populates from the new layers. This avoids
	// inflated override counts and stale entries for removed keys.
	// Resetting via Restore on a zero-value Snapshot clears the live
	// tracker without touching trackerSnap, which the rollback closure
	// above still owns. Failure paths (load/validate) call rollback,
	// which restores the pre-reload snapshot.
	c.sourceTracker.Restore(sourcetrack.Snapshot{})
	if err := c.loadSelected(ctx, selectedSources); err != nil {
		rollback(err)
		return err
	}
	if err := c.materializeEffectiveConfig(ctx); err != nil {
		materializeErr := &ConfigError{
			Op:  "Reload",
			Err: fmt.Errorf("%w: materialize effective configuration: %w", ErrConfigLoad, err),
		}
		rollback(materializeErr)
		return materializeErr
	}

	// Validate the rebuilt candidate when requested.
	// Validation runs after selected layers are rebuilt. The incremental gate
	// short-circuits only when nothing changed.
	shouldValidate := c.opts.ValidateOnLoad
	if ro.validate != nil {
		shouldValidate = *ro.validate
	}
	if err := c.validateCandidate(c.envConfig, shouldValidate); err != nil {
		rollback(err)
		return err
	}

	// A successful dry run restores the snapshots without emitting reload or
	// change metrics and events. The completion is recorded in the log.
	if ro.dryRun {
		c.envConfig = oldEnv
		c.unresolvedEnvConfig = oldUnresolvedEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		c.fileTracker.Restore(fileTrackerSnap)
		c.loaderLayers = oldLayers
		c.loaderDependencies = oldDependencies
		c.logger.Info("dry-run reload completed, changes not applied")
		return nil
	}

	// Publish the candidate only after loading and validation succeed. Change
	// callbacks and observability always see the validated state.
	c.validatedModel = nil
	duration := time.Since(start)
	if c.observer != nil {
		c.observer.RecordReload(duration)
	}

	// Snapshot everything callbacks need while the write lock is
	// held, then release the lock BEFORE iterating callbacks. This
	// guarantees a callback that calls back into the Config (Get/Set/
	// Reload/Has/etc.) cannot deadlock on c.mu. The "newEnv" snapshot
	// is also used for the post-callback "change" event so that a
	// concurrent Set/Reload landing between unlock and Emit does not
	// lie about the payload that was observed.
	callbacks := c.snapshotChangeCallbacks()
	oldFlat := dictutil.Flatten(oldEnv)
	newFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	if emitter != nil {
		emitter.Emit("reload", newEnv, duration)
	}
	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)

	if observer != nil {
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("change", oldEnv, newEnv)
	}

	return nil
}
