// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/confiify/confii-go/v2/internal/dictutil"
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
	for {
		err := c.extendAttempt(ctx, l)
		if !errors.Is(err, errConfigRevisionConflict) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (c *Config[T]) extendAttempt(ctx context.Context, l Loader) error {
	if ctx == nil {
		return &ConfigError{Op: "Extend", Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if l == nil {
		return &ConfigError{Op: "Extend", Err: fmt.Errorf("%w: nil loader", ErrConfigInvalid)}
	}

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return NewClosedError("Extend")
	}
	if c.frozen {
		c.mu.RUnlock()
		return NewFrozenError("Extend")
	}
	baseRevision := c.revision
	oldEnv := copyMap(c.envConfig)
	candidate := c.reloadSnapshotCandidate()
	observer := c.observer
	emitter := c.eventEmitter
	callbacks := append([]func(string, any, any){}, c.changeCallbacks...)
	contextCallbacks := c.snapshotChangeContextCallbacks()
	c.mu.RUnlock()

	started := time.Now()
	if err := candidate.extendCandidate(ctx, l); err != nil {
		duration := time.Since(started)
		if observer != nil {
			observer.RecordExtendFailed(duration)
		}
		if emitter != nil {
			emitter.EmitWithContext(ctx, "extend_failed", err, duration)
		}
		return err
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
		return NewClosedError("Extend")
	}
	if c.revision != baseRevision {
		c.mu.Unlock()
		return errConfigRevisionConflict
	}
	c.envConfig = newEnv
	c.unresolvedEnvConfig = copyMap(candidate.unresolvedEnvConfig)
	c.mergedConfig = copyMap(candidate.mergedConfig)
	c.loaders = append([]Loader(nil), candidate.loaders...)
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
		observer.RecordExtend(duration)
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "extend", copyMap(newEnv), duration)
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
