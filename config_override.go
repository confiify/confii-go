// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/observe"
	"github.com/confiify/confii-go/v2/sourcetrack"
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
// Calling restore after [Config.Close] is a no-op: the closed snapshot
// remains immutable and no callbacks or events are delivered.
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

	// Copy and sort the payload before running hooks. This prevents later caller
	// mutation from changing an active frame and gives multi-key hooks a stable
	// processing order.
	keys := make([]string, 0, len(overrides))
	rawOverrides := make(map[string]any, len(overrides))
	for key, value := range overrides {
		keys = append(keys, key)
		rawOverrides[key] = dictutil.DeepCopyValue(value)
	}
	sort.Strings(keys)

	effectiveOverrides := make(map[string]any, len(rawOverrides))
	for _, key := range keys {
		resolved, resolveErr := c.materializeEffectiveValue(ctx, key, rawOverrides[key])
		if resolveErr != nil {
			return nil, &ConfigError{
				Op:   "Override",
				Code: ConfigErrorCodeLoad,
				Err:  fmt.Errorf("materialize %q: %w", key, resolveErr),
			}
		}
		effectiveOverrides[key] = resolved
	}
	for {
		c.mu.RLock()
		if c.closed {
			c.mu.RUnlock()
			return nil, NewClosedError("Override")
		}
		baseRevision := c.revision
		preOverrideEnv := copyMap(c.envConfig)
		candidate := c.snapshotRuntimeMutationCandidate()

		failedKey := ""
		var candidateErr error
		for _, key := range keys {
			if candidateErr = candidate.set(key, rawOverrides[key], effectiveOverrides[key], "override", c.env); candidateErr != nil {
				failedKey = key
				break
			}
		}
		c.mu.RUnlock()
		if candidateErr == nil {
			candidateErr = c.validateMaterializedCandidate(candidate.envConfig)
		}
		if candidateErr != nil {
			conflict, conflictErr := c.runtimeMutationConflict(ctx, "Override", baseRevision)
			if conflictErr != nil {
				return nil, conflictErr
			}
			if conflict {
				continue
			}
			c.recordOverrideFailure(ctx, failedKey, candidateErr)
			if failedKey != "" {
				return nil, NewInvalidError("Override", failedKey, candidateErr)
			}
			return nil, candidateErr
		}

		frame := &overrideFrame{
			payload:          dictutil.DeepCopy(rawOverrides),
			effectivePayload: dictutil.DeepCopy(effectiveOverrides),
			applied:          true,
		}
		newEnv := copyMap(candidate.envConfig)

		c.mu.Lock()
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			return nil, err
		}
		if c.closed {
			c.mu.Unlock()
			return nil, NewClosedError("Override")
		}
		if c.revision != baseRevision {
			c.mu.Unlock()
			continue
		}

		// Capture the base only for a candidate that is certain to publish.
		// A rejected first frame therefore cannot leave hidden base state behind.
		if len(c.overrideStack) == 0 {
			c.overrideBaseEnv = dictutil.DeepCopy(c.envConfig)
			rawBase := c.unresolvedEnvConfig
			if rawBase == nil {
				rawBase = c.envConfig
			}
			c.overrideBaseRawEnv = dictutil.DeepCopy(rawBase)
			c.overrideBaseMerged = dictutil.DeepCopy(c.mergedConfig)
			c.overrideBaseTracker = c.sourceTracker.Snapshot()
			c.overrideBaseSensitive = cloneSensitivePaths(c.sensitivePaths)
			c.overrideBaseFrozen = c.frozen
		}
		c.overrideIDCounter++
		frame.id = c.overrideIDCounter
		c.overrideStack = append(c.overrideStack, frame)
		c.frozen = false
		c.publishRuntimeMutationCandidate(candidate)
		change := c.captureCommittedChange(preOverrideEnv, newEnv)
		c.mu.Unlock()

		c.deliverCommittedChange(ctx, change, func(observer *observe.Metrics) {
			observer.RecordOverride()
		}, "override", dictutil.DeepCopy(rawOverrides))
		return c.makeOverrideRestore(frame), nil
	}
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
		// A closed Config is a final immutable snapshot. Restore is a
		// mutation, so after Close it becomes a no-op rather than
		// republishing state or emitting lifecycle signals.
		if c.closed || !frame.applied {
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
			c.sensitivePaths = cloneSensitivePaths(c.overrideBaseSensitive)
			c.frozen = c.overrideBaseFrozen
			c.overrideBaseEnv = nil
			c.overrideBaseRawEnv = nil
			c.overrideBaseMerged = nil
			c.overrideBaseTracker = sourcetrack.Snapshot{}
			c.overrideBaseSensitive = nil
		} else {
			// Surviving frames are replayed from a fresh copy of the base in
			// push order. Replay remains best-effort because Set or Reload may
			// have reshaped one snapshot view while the override scope was active.
			c.envConfig = dictutil.DeepCopy(c.overrideBaseEnv)
			rawBase := c.overrideBaseRawEnv
			if rawBase == nil {
				rawBase = c.overrideBaseEnv
			}
			c.unresolvedEnvConfig = dictutil.DeepCopy(rawBase)
			c.mergedConfig = dictutil.DeepCopy(c.overrideBaseMerged)
			c.sourceTracker.Restore(c.overrideBaseTracker)
			c.sensitivePaths = cloneSensitivePaths(c.overrideBaseSensitive)
			for _, f := range c.overrideStack {
				keys := make([]string, 0, len(f.payload))
				for key := range f.payload {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, k := range keys {
					v := f.payload[k]
					effectiveValue, materialized := f.effectivePayload[k]
					if !materialized {
						effectiveValue = v
					}
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
								"override replay skipped key on merged configuration",
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
			c.sensitivePaths = sensitivePathsForConfig(c.unresolvedEnvConfig, c.opts.SensitivePaths)
			c.frozen = false
		}
		c.validatedModel = nil
		c.revision++

		newEnv := copyMap(c.envConfig)
		change := c.captureCommittedChange(preRestoreEnv, newEnv)

		c.mu.Unlock()

		// Deep-copy the payload: copyMap shares slice values with the
		// republished live state, and listeners must not mutate it.
		c.deliverCommittedChange(context.Background(), change, func(observer *observe.Metrics) {
			observer.RecordOverrideRestored()
		}, "override_restored", dictutil.DeepCopy(newEnv))
	}
}
