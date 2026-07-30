// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/confiify/confii-go/v2/compose"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/sourcetrack"
)

var errConfigRevisionConflict = errors.New("configuration revision changed")

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
	for {
		err := c.reloadAttempt(ctx, opts...)
		if !errors.Is(err, errConfigRevisionConflict) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (c *Config[T]) reloadAttempt(ctx context.Context, opts ...ReloadOption) error {
	if ctx == nil {
		return &ConfigError{Op: "Reload", Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return NewClosedError("Reload")
	}
	if c.frozen {
		c.mu.RUnlock()
		return NewFrozenError("Reload")
	}
	if reloadIsIncremental(opts) && !c.reloadHasSelectedSources() {
		c.mu.RUnlock()
		return nil
	}
	oldEnv := copyMap(c.envConfig)
	baseRevision := c.revision
	candidate := c.reloadSnapshotCandidate()
	observer := c.observer
	emitter := c.eventEmitter
	callbacks := append([]func(string, any, any){}, c.changeCallbacks...)
	contextCallbacks := c.snapshotChangeContextCallbacks()
	c.mu.RUnlock()

	started := time.Now()
	if err := candidate.reloadCandidate(ctx, opts...); err != nil {
		duration := time.Since(started)
		if observer != nil {
			observer.RecordReloadFailed(duration)
		}
		if emitter != nil {
			emitter.EmitWithContext(ctx, "reload_failed", err, duration)
		}
		return err
	}
	if reloadIsDryRun(opts) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	newEnv := copyMap(candidate.envConfig)
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	if c.closed {
		c.mu.Unlock()
		return NewClosedError("Reload")
	}
	if c.revision != baseRevision {
		c.mu.Unlock()
		return errConfigRevisionConflict
	}
	c.envConfig = newEnv
	c.unresolvedEnvConfig = copyMap(candidate.unresolvedEnvConfig)
	c.mergedConfig = copyMap(candidate.mergedConfig)
	c.loaderLayers = copyLoaderLayers(candidate.loaderLayers)
	c.loaderDependencies = copyLoaderDependencies(candidate.loaderDependencies)
	c.sourceTracker.Restore(candidate.sourceTracker.Snapshot())
	c.fileTracker.Restore(candidate.fileTracker.Snapshot())
	c.sourcePlan = cloneSourcePlan(candidate.sourcePlan)
	c.validatedModel = nil
	c.revision++
	c.mu.Unlock()

	duration := time.Since(started)
	if observer != nil {
		observer.RecordReload(duration)
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "reload", copyMap(newEnv), duration)
	}
	oldFlat, newFlat := dictutil.Flatten(oldEnv), dictutil.Flatten(newEnv)
	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)
	c.notifyContextChangesUnlocked(ctx, contextCallbacks, oldFlat, newFlat)
	if observer != nil {
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "change", oldEnv, copyMap(newEnv))
	}
	return nil
}

func reloadIsIncremental(opts []ReloadOption) bool {
	ro := reloadOpts{incremental: true}
	for _, option := range opts {
		option(&ro)
	}
	return ro.incremental
}

func reloadIsDryRun(opts []ReloadOption) bool {
	ro := reloadOpts{incremental: true}
	for _, option := range opts {
		option(&ro)
	}
	return ro.dryRun
}

// reloadHasSelectedSources is called while c.mu is read-locked.
func (c *Config[T]) reloadHasSelectedSources() bool {
	for _, loader := range c.loaders {
		source := loader.Source()
		if !c.fileTracker.IsTrackable(source) || c.fileTracker.HasChanged(source) {
			return true
		}
	}
	for _, dependencies := range c.loaderDependencies {
		for _, dependency := range dependencies {
			if c.fileTracker.HasChanged(dependency) {
				return true
			}
		}
	}
	return false
}

// reloadSnapshotCandidate is called while c.mu is read-locked.
func (c *Config[T]) reloadSnapshotCandidate() *Config[T] {
	tracker := sourcetrack.NewTracker(c.opts.DebugMode)
	tracker.Restore(c.sourceTracker.Snapshot())
	fileTracker := sourcetrack.NewFileTracker()
	fileTracker.Restore(c.fileTracker.Snapshot())
	base := c.opts.WorkingDir
	if base == "" {
		base = "."
	}
	return &Config[T]{
		unresolvedEnvConfig: copyMap(c.unresolvedEnvConfig),
		envConfig:           copyMap(c.envConfig),
		mergedConfig:        copyMap(c.mergedConfig),
		frozen:              c.frozen,
		env:                 c.env,
		loaders:             append([]Loader(nil), c.loaders...),
		loaderLayers:        copyLoaderLayers(c.loaderLayers),
		loaderDependencies:  copyLoaderDependencies(c.loaderDependencies),
		merger:              c.merger,
		hookProcessor:       c.hookProcessor,
		envHandler:          c.envHandler,
		sourceTracker:       tracker,
		fileTracker:         fileTracker,
		composer:            compose.New(base, compose.WithMerger(c.merger)),
		opts:                c.opts,
		logger:              c.logger,
		sourcePlan:          cloneSourcePlan(c.sourcePlan),
		jsonSchema:          c.jsonSchema,
	}
}
