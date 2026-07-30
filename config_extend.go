// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/observe"
	"github.com/confiify/confii-go/v2/validate"
)

// Extend adds an additional loader at runtime and merges its config into
// the live state.
//
// The loader output is composed, resolved for the active environment,
// materialized, and validated before publication. Any failure preserves the
// previous configuration, source metadata, and typed snapshot. Successful
// extension emits the extend and change signals. Callbacks run without the
// configuration lock and may safely call Config methods.
func (c *Config[T]) extendCandidate(ctx context.Context, l Loader) error {
	c.mu.Lock()
	// Use a manual unlock flag because callbacks run after the lock is released.
	// Failure paths fall through the deferred fallback; the success
	// path manually unlocks before invoking change callbacks so
	// callbacks may call back into the Config without deadlocking.
	unlocked := false
	var failureEventErr error
	var failureEventDuration time.Duration
	var failureEmitter *observe.EventEmitter
	defer func() {
		if !unlocked {
			c.mu.Unlock()
		}
		if failureEventErr != nil && failureEmitter != nil {
			failureEmitter.Emit("extend_failed", failureEventErr, failureEventDuration)
		}
	}()

	if c.frozen {
		return NewFrozenError("Extend")
	}
	if c.closed {
		return NewClosedError("Extend")
	}

	// Snapshot live state for transactional rollback.
	oldEnv := copyMap(c.envConfig)
	oldUnresolvedEnv := copyMap(c.unresolvedEnvConfig)
	oldMerged := copyMap(c.mergedConfig)
	trackerSnap := c.sourceTracker.Snapshot()
	oldLayers := copyLoaderLayers(c.loaderLayers)
	oldDependencies := copyLoaderDependencies(c.loaderDependencies)
	start := time.Now()

	// rollback restores the complete transaction state before reporting a
	// load, composition, materialization, or validation failure.
	rollback := func(failureErr error) {
		c.envConfig = oldEnv
		c.unresolvedEnvConfig = oldUnresolvedEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		c.loaderLayers = oldLayers
		c.loaderDependencies = oldDependencies
		dur := time.Since(start)
		if c.observer != nil {
			c.observer.RecordExtendFailed(dur)
		}
		failureEventErr = failureErr
		failureEventDuration = dur
		failureEmitter = c.eventEmitter
	}

	// Load the new source.
	data, err := l.Load(ctx)
	if err != nil {
		if cancellation := operationCancellation(ctx, err); cancellation != nil {
			rollback(cancellation)
			return cancellation
		}
		switch c.opts.OnError {
		case ErrorPolicyRaise:
			rollback(err)
			return err
		case ErrorPolicyWarn:
			c.logger.Warn(
				"loader error",
				slog.String("source", l.Source()),
				slog.String("error", err.Error()),
			)
			// Warn skips the loader: nothing to commit, but no failure
			// either. Snapshots are not restored because nothing was
			// mutated; return without commit-time observability.
			return nil
		case ErrorPolicyIgnore:
			// Silent: distinct from Warn, do not emit a log record.
			return nil
		default:
			// Unknown policy values are treated as Raise so that
			// misconfiguration surfaces loudly, mirroring c.load.
			rollback(err)
			return err
		}
	}
	if data == nil {
		// Graceful absence: the loader had nothing to contribute. No
		// snapshot rollback needed because nothing was mutated, and no
		// commit-time observability fires.
		return nil
	}

	// Process _include, _defaults, and _merge_strategy directives. Composition
	// errors follow the configured error policy.
	composed, dependencies, err := c.composer.ComposeWithDependenciesWithContext(ctx, data, l.Source())
	if err != nil {
		if cancellation := operationCancellation(ctx, err); cancellation != nil {
			rollback(cancellation)
			return cancellation
		}
		switch c.opts.OnError {
		case ErrorPolicyRaise:
			rollback(err)
			return err
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
			rollback(err)
			return err
		}
	}

	// Resolve default and active-environment sections using the same handler as
	// initial loading.
	resolved := c.envHandler.Resolve(composed, c.env)

	// Merge into candidate state. A later failure restores the snapshot.
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
		rollback(materializeErr)
		return materializeErr
	}

	// Validate the candidate when validation-on-load is enabled.
	//
	// A JSON Schema (inline map or compiled file path) is
	// honored in addition to the struct path. Schema violations roll
	// back via the shared closure with a sanitized public message and
	// the structured violation list on Context["schema_errors"].
	if c.opts.ValidateOnLoad {
		if c.jsonSchema != nil {
			msgs, serr := c.jsonSchema.ValidateDetailed(c.envConfig)
			if serr != nil {
				count := max(1, len(msgs))
				validationErr := &ConfigError{
					Op: "Extend",
					Err: fmt.Errorf(
						"%w: schema validation failed for %d constraint(s)",
						ErrConfigValidation, count,
					),
					Context: map[string]any{
						"schema_errors": msgs,
					},
				}
				rollback(validationErr)
				return validationErr
			}
		}
		if configTypeSupportsStructValidation[T]() {
			if _, verr := validate.DecodeAndValidate[T](c.envConfig); verr != nil {
				validationErr := NewValidationError([]string{verr.Error()}, verr)
				rollback(validationErr)
				return validationErr
			}
		}
	}

	// Commit only after the candidate has loaded, composed, and validated.
	// The commit updates non-rollback-protected
	// state (loaders slice, file tracker, source tracker for the new
	// loader and validated-model cache invalidation) and emits commit-time
	// observability.
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

	c.validatedModel = nil
	c.revision++
	duration := time.Since(start)
	if c.observer != nil {
		c.observer.RecordExtend(duration)
	}

	// Snapshot callbacks and flattened state under the write lock, then release
	// the lock before dispatching callbacks.
	callbacks := c.snapshotChangeCallbacks()
	oldFlat := dictutil.Flatten(oldEnv)
	newFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	if emitter != nil {
		emitter.Emit("extend", newEnv, duration)
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

// trackCompositionDependency registers an included file for incremental
// reloads. Keeping the best-effort failure handling in a small helper makes
// the TOCTOU case (the include disappears after composition) deterministic to
// test without weakening Extend's transaction semantics.
func (c *Config[T]) trackCompositionDependency(dependency string) {
	if ferr := c.fileTracker.Track(dependency); ferr != nil {
		c.logger.Debug("extend: composition dependency not tracked", slog.String("source", dependency), slog.String("reason", ferr.Error()))
	}
}
