// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"

	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/sourcetrack"
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

// Override temporarily overrides configuration values.
// Returns a restore function that must be called (typically via defer) to revert.
//
// G13 (F-G13-Override): Override fires registered OnChange callbacks
// for every key whose value differs between the pre-override and
// post-override flat state. Callbacks observe the deletion contract
// uniformly with [Config.Reload] and [Config.Extend]: a key whose
// value is replaced surfaces (oldVal, newVal); a key that did not
// exist before but is introduced by the override surfaces
// (nil, newVal). Callbacks fire AFTER c.mu has been released so a
// callback that calls back into the Config cannot deadlock.
//
// The restore function returned from Override likewise fires
// OnChange callbacks for every key that the restoration mutates,
// so observers can react symmetrically to override / restore cycles.
// Restore-time callbacks run with c.mu released for the same
// deadlock-safety reason.
//
// Override is LIFO-composable: each call pushes a frame onto an
// internal stack, and the returned restore removes its own frame
// regardless of stack position. While the stack is non-empty, live
// envConfig / mergedConfig / source tracker are derived by replaying
// remaining frames onto the captured base; a fully-drained stack
// returns the Config to its pre-Override state. The closure is
// idempotent — a second call is a no-op.
//
// Out-of-order restore (popping a non-top frame) is supported. The
// rebuild calls TrackValue on each surviving frame, which inflates
// OverrideCount on those keys; the alternative — per-frame inverse-
// delta bookkeeping — was rejected as overhead for an already-rare
// path.
//
// An unrestored frame keeps c.frozen = false (Override clears it to
// permit nested overrides). Callers that have not relinquished an
// override scope cannot Freeze the Config.
func (c *Config[T]) Override(overrides map[string]any) (restore func(), err error) {
	// Materialize every candidate before locking or mutating live state.
	// Provider failures reject the complete override and preserve the current
	// ready snapshot.
	effectiveOverrides := make(map[string]any, len(overrides))
	for key, value := range overrides {
		resolved, resolveErr := c.materializeEffectiveValue(context.Background(), key, value)
		if resolveErr != nil {
			return nil, &ConfigError{
				Op:  "Override",
				Err: fmt.Errorf("%w: materialize %q: %w", ErrConfigLoad, key, resolveErr),
			}
		}
		effectiveOverrides[key] = resolved
	}
	c.mu.Lock()

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
			c.mu.Unlock()
			return nil, NewInvalidError("Override", k, serr)
		}
		if serr := dictutil.SetNested(c.mergedConfig, k, stored); serr != nil {
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
	c.overrideStack = append(c.overrideStack, frame)
	c.validatedModel = nil

	// Snapshot the callback list and pre/post flat state under the
	// write lock; iterate callbacks after release so a callback that
	// re-enters the Config cannot deadlock on c.mu.
	overrideCallbacks := c.snapshotChangeCallbacks()
	overrideOldFlat := dictutil.Flatten(preOverrideEnv)
	overrideNewFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()

	c.notifyChangesUnlocked(overrideCallbacks, overrideOldFlat, overrideNewFlat)

	if observer != nil {
		observer.RecordOverride()
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("override", overrides)
		emitter.Emit("change", preOverrideEnv, newEnv)
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
