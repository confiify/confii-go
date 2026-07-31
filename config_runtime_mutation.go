// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/sourcetrack"
)

// runtimeMutationCandidate owns the state changed by Set and Override. It is
// built from a published revision without changing live state, validated
// without holding the live Config lock, and discarded unless that revision can
// still be committed.
type runtimeMutationCandidate struct {
	envConfig           map[string]any
	unresolvedEnvConfig map[string]any
	mergedConfig        map[string]any
	sourceTracker       *sourcetrack.Tracker
}

// snapshotRuntimeMutationCandidate must be called while c.mu is read-locked.
func (c *Config[T]) snapshotRuntimeMutationCandidate() *runtimeMutationCandidate {
	raw := c.unresolvedEnvConfig
	if raw == nil {
		raw = c.envConfig
	}
	return newRuntimeMutationCandidate(
		c.envConfig,
		raw,
		c.mergedConfig,
		c.sourceTracker.Snapshot(),
		c.opts.DebugMode,
	)
}

func newRuntimeMutationCandidate(
	envConfig map[string]any,
	unresolvedEnvConfig map[string]any,
	mergedConfig map[string]any,
	trackerSnapshot sourcetrack.Snapshot,
	debugMode bool,
) *runtimeMutationCandidate {
	tracker := sourcetrack.NewTracker(debugMode)
	tracker.Restore(trackerSnapshot)
	return &runtimeMutationCandidate{
		envConfig:           dictutil.DeepCopy(envConfig),
		unresolvedEnvConfig: dictutil.DeepCopy(unresolvedEnvConfig),
		mergedConfig:        dictutil.DeepCopy(mergedConfig),
		sourceTracker:       tracker,
	}
}

// set applies one raw/effective value pair to a private candidate. A path error
// may partially modify the candidate because SetNested can create intermediate
// maps before failing; callers must discard that candidate on every error.
func (candidate *runtimeMutationCandidate) set(
	key string,
	rawValue any,
	effectiveValue any,
	source string,
	environment string,
) error {
	if err := dictutil.SetNested(candidate.envConfig, key, dictutil.DeepCopyValue(effectiveValue)); err != nil {
		return err
	}
	if err := dictutil.SetNested(candidate.unresolvedEnvConfig, key, dictutil.DeepCopyValue(rawValue)); err != nil {
		return err
	}
	if err := dictutil.SetNested(candidate.mergedConfig, key, dictutil.DeepCopyValue(rawValue)); err != nil {
		return err
	}

	candidate.sourceTracker.TrackValue(key, rawValue, source, source, environment)
	return nil
}

// publishRuntimeMutationCandidate replaces only runtime-mutable snapshot state.
// The caller must hold c.mu for writing and verify the candidate revision first.
func (c *Config[T]) publishRuntimeMutationCandidate(candidate *runtimeMutationCandidate) {
	c.envConfig = candidate.envConfig
	c.unresolvedEnvConfig = candidate.unresolvedEnvConfig
	c.mergedConfig = candidate.mergedConfig
	c.sourceTracker.Restore(candidate.sourceTracker.Snapshot())
	c.validatedModel = nil
	c.revision++
}

// runtimeMutationConflict reports whether an unpublished candidate became
// stale. Stable failures are returned to the caller; stale failures are retried
// against a new snapshot so errors never describe an obsolete tree shape.
func (c *Config[T]) runtimeMutationConflict(ctx context.Context, operation string, revision uint64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return false, NewClosedError(operation)
	}
	return c.revision != revision, nil
}

func (c *Config[T]) recordSetFailure(ctx context.Context, key string, failure error) {
	c.mu.RLock()
	observer, emitter := c.observer, c.eventEmitter
	c.mu.RUnlock()
	if observer != nil {
		observer.RecordSetFailed()
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "set_failed", key, failure)
	}
}

func (c *Config[T]) recordOverrideFailure(ctx context.Context, key string, failure error) {
	c.mu.RLock()
	observer, emitter := c.observer, c.eventEmitter
	c.mu.RUnlock()
	if observer != nil {
		observer.RecordOverrideFailed()
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "override_failed", key, failure)
	}
}
