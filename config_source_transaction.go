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
	"github.com/confiify/confii-go/v2/observe"
	"github.com/confiify/confii-go/v2/sourcetrack"
)

var errConfigRevisionConflict = errors.New("configuration revision changed")

type sourceTransactionOperation struct {
	name  string
	event string
}

var (
	reloadTransaction = sourceTransactionOperation{name: "Reload", event: "reload"}
	extendTransaction = sourceTransactionOperation{name: "Extend", event: "extend"}
)

// sourceTransactionOutcome distinguishes a successfully prepared publication
// from successful operations that intentionally leave live state unchanged,
// such as an incremental no-op, a dry run, or an ignored empty source.
type sourceTransactionOutcome struct {
	publish bool
}

type sourceCandidatePreparer[T any] func(context.Context, *Config[T]) (sourceTransactionOutcome, error)

// runSourceTransaction retries optimistic source transactions when another
// mutation publishes while the candidate is being prepared. Candidate work
// never holds the live configuration lock, so readers continue to observe the
// previous complete snapshot throughout loader and provider I/O.
func (c *Config[T]) runSourceTransaction(
	ctx context.Context,
	operation sourceTransactionOperation,
	prepare sourceCandidatePreparer[T],
) error {
	if ctx == nil {
		return &ConfigError{Op: operation.name, Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	for {
		err := c.sourceTransactionAttempt(ctx, operation, prepare)
		if !errors.Is(err, errConfigRevisionConflict) {
			return err
		}
	}
}

func (c *Config[T]) sourceTransactionAttempt(
	ctx context.Context,
	operation sourceTransactionOperation,
	prepare sourceCandidatePreparer[T],
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return NewClosedError(operation.name)
	}
	if c.frozen {
		c.mu.RUnlock()
		return NewFrozenError(operation.name)
	}
	baseRevision := c.revision
	oldEnv := copyMap(c.envConfig)
	candidate := c.snapshotSourceCandidate()
	c.mu.RUnlock()

	started := time.Now()
	outcome, err := prepare(ctx, candidate)
	if err != nil {
		c.recordSourceTransactionFailure(ctx, operation, err, time.Since(started))
		return err
	}
	if !outcome.publish {
		return nil
	}

	newEnv := copyMap(candidate.envConfig)
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	if c.closed {
		c.mu.Unlock()
		return NewClosedError(operation.name)
	}
	// Freeze can race with long-running loader or provider work without changing
	// revision. Rechecking it at publication prevents a transaction admitted
	// before Freeze from committing afterward.
	if c.frozen {
		c.mu.Unlock()
		return NewFrozenError(operation.name)
	}
	if c.revision != baseRevision {
		c.mu.Unlock()
		return errConfigRevisionConflict
	}

	c.publishSourceCandidate(candidate)
	callbacks := c.snapshotChangeCallbacks()
	contextCallbacks := c.snapshotChangeContextCallbacks()
	observer := c.observer
	emitter := c.eventEmitter
	c.mu.Unlock()

	duration := time.Since(started)
	recordSourceTransactionSuccess(observer, operation, duration)
	if emitter != nil {
		emitter.EmitWithContext(ctx, operation.event, copyMap(newEnv), duration)
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

// publishSourceCandidate replaces all state derived from source loading. The
// caller must hold c.mu for writing and must have completed revision and
// lifecycle checks immediately before calling it.
func (c *Config[T]) publishSourceCandidate(candidate *Config[T]) {
	c.unresolvedEnvConfig = copyMap(candidate.unresolvedEnvConfig)
	c.envConfig = copyMap(candidate.envConfig)
	c.mergedConfig = copyMap(candidate.mergedConfig)
	c.loaders = append([]Loader(nil), candidate.loaders...)
	c.loaderLayers = copyLoaderLayers(candidate.loaderLayers)
	c.loaderDependencies = copyLoaderDependencies(candidate.loaderDependencies)
	c.sourceTracker.Restore(candidate.sourceTracker.Snapshot())
	c.fileTracker.Restore(candidate.fileTracker.Snapshot())
	c.sourcePlan = cloneSourcePlan(candidate.sourcePlan)
	c.validatedModel = nil
	c.revision++
}

func (c *Config[T]) recordSourceTransactionFailure(
	ctx context.Context,
	operation sourceTransactionOperation,
	failure error,
	duration time.Duration,
) {
	c.mu.RLock()
	observer := c.observer
	emitter := c.eventEmitter
	c.mu.RUnlock()
	if observer != nil {
		switch operation {
		case reloadTransaction:
			observer.RecordReloadFailed(duration)
		case extendTransaction:
			observer.RecordExtendFailed(duration)
		}
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, operation.event+"_failed", failure, duration)
	}
}

func recordSourceTransactionSuccess(observer *observe.Metrics, operation sourceTransactionOperation, duration time.Duration) {
	if observer == nil {
		return
	}
	switch operation {
	case reloadTransaction:
		observer.RecordReload(duration)
	case extendTransaction:
		observer.RecordExtend(duration)
	}
}

// snapshotSourceCandidate is called while c.mu is read-locked. Collaborators
// whose configuration is immutable after New are shared; all source-derived
// maps, slices, and trackers that candidate preparation mutates are copied.
func (c *Config[T]) snapshotSourceCandidate() *Config[T] {
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
		exporters:           c.exporters,
		opts:                c.opts,
		logger:              c.logger,
		sourcePlan:          cloneSourcePlan(c.sourcePlan),
		jsonSchema:          c.jsonSchema,
	}
}
