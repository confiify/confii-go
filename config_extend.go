// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/observe"
	"github.com/confiify/confii-go/validate"
	"log/slog"
	"time"
)

// Extend adds an additional loader at runtime and merges its config into
// the live state.
//
// Extend's lifecycle mirrors [Config.Reload]'s seven-phase pipeline so
// that runtime extension and reload share the same composition,
// environment resolution, file tracking, validation, snapshot/rollback,
// and observability semantics. Pre-G15, Extend was a partial merge path
// that bypassed all of those — calling Extend with a YAML file that
// contained a top-level "default:" map or "_include:" directive
// silently dropped the directive instead of resolving it. Post-G15:
//
//  1. Snapshot: live envConfig, mergedConfig, and source tracker are
//     captured so any failure restores them in lockstep (D05 / G14).
//  2. Load: l.Load(ctx) is invoked. Errors are dispatched through
//     c.opts.OnError (Raise → return, Warn → log+skip, Ignore →
//     silent-skip) exactly like the [Config.load] pipeline.
//  3. Compose: composition directives ("_include", "_defaults",
//     "_merge_strategy") in the loaded data are processed via
//     c.composer.Compose. Errors flow through the same OnError
//     dispatch as the loader phase.
//  4. Env-resolve: c.envHandler.Resolve folds env-keyed sections
//     (default + active env) so Extend honors the same flat-vs-env
//     contract that [Config.load] does. Pre-G15, an env-keyed file
//     passed to Extend would surface its raw "default:"/"production:"
//     branches directly into envConfig.
//  5. Merge: the resolved overlay is merged into both mergedConfig
//     and envConfig (envConfig because the overlay is already env-
//     resolved and should apply on top of the resolved state).
//  6. Validate: when c.opts.ValidateOnLoad is true and a schema is
//     configured, the new state is decoded into T and validated.
//     Failure rolls back every snapshot and returns a typed
//     [ErrConfigValidation].
//  7. Commit: only after every preceding phase succeeds do we append
//     to c.loaders, register the source with c.fileTracker (file-based
//     loaders only; non-file sources are silently skipped, pending the
//     G20 source-capability model), invalidate c.validatedModel,
//     update c.sourceTracker with the resolved overlay, emit the
//     "extend" metric and event, run change callbacks, and emit
//     "change". A "change" event paired with the extend overlay
//     payload allows operators to subscribe to runtime extensions
//     uniformly with reloads.
//
// On any failure path inside phases 2–6, c.observer.RecordExtendFailed
// is called and the "extend_failed" event is emitted; the original
// error is returned to the caller.
//
// G11 cache invalidation contract: c.validatedModel is set to nil only
// on commit. A failed Extend leaves the previous validated model
// reachable for [Config.Typed].
//
// G13 callback semantics: change callbacks are invoked AFTER c.mu has
// been released. A snapshot of the callback list, the pre-extend flat
// state, and the post-extend flat state is taken under the write lock
// in the commit phase; the lock is then dropped before
// notifyChangesUnlocked iterates the snapshots. This guarantees a
// callback that calls back into the Config (Get/Set/Reload/Extend/etc.)
// cannot deadlock against the write lock Extend held during the
// pipeline. Removed keys are reported uniformly with the Reload path
// (oldVal != nil, newVal == nil).
func (c *Config[T]) Extend(ctx context.Context, l Loader) error {
	c.mu.Lock()
	// G13: see Reload for the rationale behind the manual unlock flag.
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

	// Phase 1: Snapshot live state for rollback. Mirrors Reload phase 3.
	oldEnv := copyMap(c.envConfig)
	oldMerged := copyMap(c.mergedConfig)
	trackerSnap := c.sourceTracker.Snapshot()
	oldLayers := copyLoaderLayers(c.loaderLayers)
	oldDependencies := copyLoaderDependencies(c.loaderDependencies)
	start := time.Now()

	// rollback restores every snapshot taken in phase 1 and drives the
	// failure-path observability hooks. It is a closure so every
	// failure exit (load, compose, validate) shares the same restoration
	// logic, matching the Reload rollback pattern.
	rollback := func(failureErr error) {
		c.envConfig = oldEnv
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

	// Phase 2: Load.
	data, err := l.Load(ctx)
	if err != nil {
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
			// mutated; we simply return without commit-time observability.
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

	// Phase 3: Compose. Process _include / _defaults / _merge_strategy
	// directives. Errors flow through OnError, mirroring c.load.
	composed, dependencies, err := c.composer.ComposeWithDependencies(data, l.Source())
	if err != nil {
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

	// Phase 4: Env-resolve. The composer output may carry env-keyed
	// sections (default / <env>); honor them through the same handler
	// c.load uses so Extend and load agree on the env-handling contract.
	resolved := c.envHandler.Resolve(composed, c.env)

	// Phase 5: Merge into live state. Compute the new envConfig /
	// mergedConfig as candidates so a later validation failure can be
	// rolled back via the snapshot taken in phase 1.
	c.mergedConfig = c.merger.Merge(c.mergedConfig, composed)
	c.envConfig = c.merger.Merge(c.envConfig, resolved)

	// Phase 6: Validate. When validate-on-load is configured AND a
	// schema is present, decode and validate the new state. Pre-G15
	// Extend skipped this entirely, so an extension that violated the
	// validator was silently committed.
	//
	// G01: a JSON Schema (inline map or compiled file path) is now
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

	// Phase 7: Commit. Only now do we mutate non-rollback-protected
	// state (loaders slice, file tracker, source tracker for the new
	// loader, validated-model cache invalidation) and emit commit-time
	// observability.
	c.loaders = append(c.loaders, l)
	c.loaderLayers = append(c.loaderLayers, copyMap(composed))
	c.loaderDependencies = append(c.loaderDependencies, append([]string(nil), dependencies...))

	// Track the source with the file tracker so subsequent Reload calls
	// can detect changes to it. Non-file sources (HTTP, env, etc.)
	// produce a Stat error which we silently ignore — the G20 source-
	// capability model will replace this best-effort approach.
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
	duration := time.Since(start)
	if c.observer != nil {
		c.observer.RecordExtend(duration)
	}

	// G13: snapshot callbacks + flat state under the write lock, then
	// release the lock before iterating callbacks. Mirrors the Reload
	// commit phase so the deadlock and deletion-miss fix applies
	// symmetrically to runtime extension.
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
