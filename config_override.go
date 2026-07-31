// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/sourcetrack"
	"log/slog"
)

// overrideFrame is a single entry in [Config.overrideStack]. The
// applied flag is flipped on the first restore invocation, so a
// second call is a no-op.
type overrideFrame struct {
	id               uint64
	payload          map[string]any
	effectivePayload map[string]any
	applied          bool
}

// Override temporarily applies dot-separated key/value overrides and returns
// an idempotent restore function. Callers should normally defer restoration:
//
//	restore, err := cfg.Override(map[string]any{"server.port": 9090})
//	if err != nil { return err }
//	defer restore()
//
// Values are materialized before publication and caller-owned maps and slices
// are copied. The operation is atomic: an invalid path, hook failure, secret
// provider failure, or closed Config returns an error without changing the
// current snapshot. Both application and restoration emit [Config.OnChange]
// notifications after their respective commits.
//
// Overrides may be nested, and restore functions may be called in any order;
// each restore removes only its own override while preserving later active
// overrides. While at least one override is active, the Config remains
// mutable, even if it was frozen before the first override. Restoring the last
// override reinstates the original frozen state. Failing to call restore keeps
// the override and mutable state active for the lifetime of the Config.
func (c *Config[T]) Override(overrides map[string]any) (restore func(), err error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.OverrideWithContext(ctx, overrides)
}

// OverrideWithContext is the context-aware form of [Config.Override]. The
// context bounds hook and secret-provider work. A nil or canceled context
// returns an error, and cancellation cannot leave a partially applied
// override.
func (c *Config[T]) OverrideWithContext(ctx context.Context, overrides map[string]any) (restore func(), err error) {
	if ctx == nil {
		return nil, NewInvalidError("Override", "", errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return nil, NewClosedError("Override")
	}
	// Materialize every candidate before locking or mutating live state.
	// Provider failures reject the complete override and preserve the current
	// ready snapshot.
	effectiveOverrides := make(map[string]any, len(overrides))
	for key, value := range overrides {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, resolveErr := c.materializeEffectiveValue(ctx, key, value)
		if resolveErr != nil {
			return nil, &ConfigError{
				Op:  "Override",
				Err: fmt.Errorf("%w: materialize %q: %w", ErrConfigLoad, key, resolveErr),
			}
		}
		effectiveOverrides[key] = resolved
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if c.closed {
		c.mu.Unlock()
		return nil, NewClosedError("Override")
	}

	wasEmpty := len(c.overrideStack) == 0

	// Capture the override base on the empty → non-empty transition.
	// The base is consumed only when the stack drains; nested pushes
	// just append.
	if wasEmpty {
		c.overrideBaseEnv = dictutil.DeepCopy(c.envConfig)
		rawBase := c.unresolvedEnvConfig
		if rawBase == nil {
			rawBase = c.envConfig
		}
		c.overrideBaseRawEnv = dictutil.DeepCopy(rawBase)
		c.overrideBaseMerged = dictutil.DeepCopy(c.mergedConfig)
		c.overrideBaseTracker = c.sourceTracker.Snapshot()
		c.overrideBaseFrozen = c.frozen
	}

	preOverrideEnv := copyMap(c.envConfig)
	var preOverrideRawEnv map[string]any
	if c.unresolvedEnvConfig == nil {
		preOverrideRawEnv = copyMap(c.envConfig)
		c.unresolvedEnvConfig = copyMap(c.envConfig)
	} else {
		preOverrideRawEnv = copyMap(c.unresolvedEnvConfig)
	}
	preOverrideMerged := copyMap(c.mergedConfig)
	preOverrideTrackerSnap := c.sourceTracker.Snapshot()
	preOverrideFrozen := c.frozen
	c.frozen = false
	rollbackOverride := func() {
		c.envConfig = preOverrideEnv
		c.unresolvedEnvConfig = preOverrideRawEnv
		c.mergedConfig = preOverrideMerged
		c.frozen = preOverrideFrozen
		c.sourceTracker.Restore(preOverrideTrackerSnap)
		if wasEmpty {
			c.overrideBaseEnv = nil
			c.overrideBaseRawEnv = nil
			c.overrideBaseMerged = nil
			c.overrideBaseTracker = sourcetrack.Snapshot{}
		}
	}

	// A SetNested *PathError on any key halts the override, restores
	// the pre-Override snapshots (not the override base — surviving
	// frames below this push must remain applied), and surfaces the
	// error as a typed *ConfigError.
	frame := &overrideFrame{
		id:               c.overrideIDCounter + 1,
		payload:          make(map[string]any, len(overrides)),
		effectivePayload: make(map[string]any, len(overrides)),
		applied:          true,
	}
	c.overrideIDCounter = frame.id
	for k, v := range overrides {
		// Defensive deep copy: a caller mutating the overrides map
		// after Override returns must not bleed into Config state.
		stored := dictutil.DeepCopyValue(v)
		effectiveStored := dictutil.DeepCopyValue(effectiveOverrides[k])
		frame.payload[k] = stored
		frame.effectivePayload[k] = effectiveStored
		if serr := dictutil.SetNested(c.envConfig, k, effectiveStored); serr != nil {
			rollbackOverride()
			observer := c.observer
			emitter := c.eventEmitter
			c.mu.Unlock()
			if observer != nil {
				observer.RecordOverrideFailed()
			}
			if emitter != nil {
				emitter.Emit("override_failed", k, serr)
			}
			return nil, NewInvalidError("Override", k, serr)
		}
		if serr := dictutil.SetNested(c.unresolvedEnvConfig, k, stored); serr != nil {
			rollbackOverride()
			observer := c.observer
			emitter := c.eventEmitter
			c.mu.Unlock()
			if observer != nil {
				observer.RecordOverrideFailed()
			}
			if emitter != nil {
				emitter.EmitWithContext(ctx, "override_failed", k, serr)
			}
			return nil, NewInvalidError("Override", k, serr)
		}
		if serr := dictutil.SetNested(c.mergedConfig, k, stored); serr != nil {
			rollbackOverride()
			observer := c.observer
			emitter := c.eventEmitter
			c.mu.Unlock()
			if observer != nil {
				observer.RecordOverrideFailed()
			}
			if emitter != nil {
				emitter.Emit("override_failed", k, serr)
			}
			return nil, NewInvalidError("Override", k, serr)
		}
		// Source label "override" (vs Set's "runtime") so Explain can
		// distinguish runtime mutations from override-scope mutations.
		c.sourceTracker.TrackValue(k, stored, "override", "override", c.env)
	}
	if validationErr := c.validateMaterializedCandidate(c.envConfig); validationErr != nil {
		rollbackOverride()
		observer := c.observer
		emitter := c.eventEmitter
		c.mu.Unlock()
		if observer != nil {
			observer.RecordOverrideFailed()
		}
		if emitter != nil {
			emitter.EmitWithContext(ctx, "override_failed", validationErr)
		}
		return nil, validationErr
	}
	c.overrideStack = append(c.overrideStack, frame)
	c.validatedModel = nil
	c.revision++

	// Snapshot the callback list and pre/post flat state under the
	// write lock; iterate callbacks after release so a callback that
	// re-enters the Config cannot deadlock on c.mu.
	overrideCallbacks := c.snapshotChangeCallbacks()
	overrideContextCallbacks := c.snapshotChangeContextCallbacks()
	overrideOldFlat := dictutil.Flatten(preOverrideEnv)
	overrideNewFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()

	c.notifyChangesUnlocked(overrideCallbacks, overrideOldFlat, overrideNewFlat)
	c.notifyContextChangesUnlocked(ctx, overrideContextCallbacks, overrideOldFlat, overrideNewFlat)

	if observer != nil {
		observer.RecordOverride()
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "override", overrides)
		emitter.EmitWithContext(ctx, "change", preOverrideEnv, newEnv)
	}

	restore = c.makeOverrideRestore(frame)
	return restore, nil
}

// makeOverrideRestore returns the restore closure for frame. The
// closure removes frame from c.overrideStack regardless of position
// and rebuilds live state from the override base plus surviving
// frames in original push order. The applied flag is mutated only
// under c.mu so concurrent restores on peer frames cannot race; the
// idempotent fast path is the !applied early return.
func (c *Config[T]) makeOverrideRestore(frame *overrideFrame) func() {
	return func() {
		c.mu.Lock()
		if !frame.applied {
			c.mu.Unlock()
			return
		}
		frame.applied = false

		preRestoreEnv := copyMap(c.envConfig)

		// Remove frame from any position in the stack.
		newStack := make([]*overrideFrame, 0, len(c.overrideStack))
		for _, f := range c.overrideStack {
			if f.id == frame.id {
				continue
			}
			newStack = append(newStack, f)
		}
		c.overrideStack = newStack

		if len(c.overrideStack) == 0 {
			// Stack drained: restore base atomically. frozen returns
			// to whatever it was when the first override pushed.
			c.envConfig = c.overrideBaseEnv
			c.unresolvedEnvConfig = c.overrideBaseRawEnv
			c.mergedConfig = c.overrideBaseMerged
			c.sourceTracker.Restore(c.overrideBaseTracker)
			c.frozen = c.overrideBaseFrozen
			c.overrideBaseEnv = nil
			c.overrideBaseRawEnv = nil
			c.overrideBaseMerged = nil
			c.overrideBaseTracker = sourcetrack.Snapshot{}
		} else {
			// Surviving frames: rebuild from a fresh deep copy of the
			// base (so we do not mutate the persisted snapshot) and
			// replay each frame's payload in push order.
			c.envConfig = dictutil.DeepCopy(c.overrideBaseEnv)
			rawBase := c.overrideBaseRawEnv
			if rawBase == nil {
				rawBase = c.overrideBaseEnv
			}
			c.unresolvedEnvConfig = dictutil.DeepCopy(rawBase)
			c.mergedConfig = dictutil.DeepCopy(c.overrideBaseMerged)
			c.sourceTracker.Restore(c.overrideBaseTracker)
			for _, f := range c.overrideStack {
				for k, v := range f.payload {
					effectiveValue, materialized := f.effectivePayload[k]
					if !materialized {
						effectiveValue = v
					}
					// Replay-time SetNested failure means a Set or
					// Reload during override scope reshaped the tree;
					// log and skip. The scope is best-effort under
					// concurrent mutation.
					if serr := dictutil.SetNested(c.envConfig, k, effectiveValue); serr != nil {
						if c.logger != nil {
							c.logger.Warn(
								"override replay skipped key",
								slog.String("key", k),
								slog.Uint64("frame_id", f.id),
								slog.String("error", serr.Error()),
							)
						}
						continue
					}
					if serr := dictutil.SetNested(c.unresolvedEnvConfig, k, v); serr != nil {
						if c.logger != nil {
							c.logger.Warn(
								"override replay skipped key on unresolved environment",
								slog.String("key", k),
								slog.Uint64("frame_id", f.id),
								slog.String("error", serr.Error()),
							)
						}
						continue
					}
					if serr := dictutil.SetNested(c.mergedConfig, k, v); serr != nil {
						if c.logger != nil {
							c.logger.Warn(
								"override replay skipped key on mergedConfig",
								slog.String("key", k),
								slog.Uint64("frame_id", f.id),
								slog.String("error", serr.Error()),
							)
						}
						continue
					}
					c.sourceTracker.TrackValue(k, v, "override", "override", c.env)
				}
			}
			c.frozen = false
		}
		c.validatedModel = nil
		c.revision++

		restoreCallbacks := c.snapshotChangeCallbacks()
		restoreOldFlat := dictutil.Flatten(preRestoreEnv)
		restoreNewFlat := dictutil.Flatten(c.envConfig)
		newEnv := copyMap(c.envConfig)
		restoreObserver := c.observer
		restoreEmitter := c.eventEmitter

		c.mu.Unlock()

		c.notifyChangesUnlocked(restoreCallbacks, restoreOldFlat, restoreNewFlat)

		if restoreObserver != nil {
			restoreObserver.RecordOverrideRestored()
			restoreObserver.RecordChange()
		}
		if restoreEmitter != nil {
			restoreEmitter.Emit("override_restored", newEnv)
			restoreEmitter.Emit("change", preRestoreEnv, newEnv)
		}
	}
}
