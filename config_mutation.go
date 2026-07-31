// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"

	"github.com/confiify/confii-go/v2/internal/dictutil"
)

// SetOption configures one [Config.Set] or [Config.SetWithContext] operation.
// Options are applied in argument order; when the same setting is supplied
// more than once, the last value wins.
type SetOption func(*setOpts)

type setOpts struct{ allowOverride bool }

// WithOverride controls whether Set may replace an existing key. The default
// is true. When false, attempting to write an existing path returns an error
// and leaves the published configuration unchanged.
func WithOverride(v bool) SetOption {
	return func(o *setOpts) { o.allowOverride = v }
}

// Set materializes and validates a value, then atomically stores it at the
// dot-separated key path. It is thread-safe and respects frozen state. Pass
// WithOverride(false) to reject an existing key.
//
// Input maps and slices are copied defensively. A failed path update or failed
// materialization leaves the published snapshot and source tracking unchanged.
// Successful values are attributed to the synthetic "runtime" source and
// remain until a source-backed Reload rebuilds the snapshot. Change callbacks
// run after the internal lock is released.
func (c *Config[T]) Set(keyPath string, value any, opts ...SetOption) error {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.SetWithContext(ctx, keyPath, value, opts...)
}

// SetWithContext is the context-aware form of [Config.Set]. The context bounds
// transformation hooks and secret-provider work required to materialize value.
// A nil context, cancellation, a frozen or closed Config, an invalid path, a
// rejected overwrite, or a materialization failure returns an error and leaves
// the published snapshot unchanged. Use [errors.Is] to identify Confii error
// categories such as [ErrConfigInvalid], [ErrConfigFrozen], and
// [ErrConfigClosed].
func (c *Config[T]) SetWithContext(ctx context.Context, keyPath string, value any, opts ...SetOption) error {
	if ctx == nil {
		return NewInvalidError("Set", keyPath, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.RLock()
	closed, frozen := c.closed, c.frozen
	c.mu.RUnlock()
	if closed {
		return NewClosedError("Set")
	}
	if frozen {
		return NewFrozenError("Set")
	}
	// Resolve a private candidate before taking the Config lock. Hooks may
	// perform remote I/O or re-enter Config, and a successful Set must preserve
	// the invariant that all live reads observe an already-materialized value.
	rawStored := dictutil.DeepCopyValue(value)
	effectiveStored, materializeErr := c.materializeEffectiveValue(ctx, keyPath, rawStored)
	if materializeErr != nil {
		return &ConfigError{
			Op:  "Set",
			Err: fmt.Errorf("%w: materialize %q: %w", ErrConfigLoad, keyPath, materializeErr),
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	// Use a manual unlock flag because callbacks must run after the lock is
	// released and may safely call back into Config.
	// Failure paths fall through the deferred fallback; the success
	// path manually unlocks before invoking change callbacks so
	// callbacks may call back into the Config without deadlocking.
	unlocked := false
	defer func() {
		if !unlocked {
			c.mu.Unlock()
		}
	}()

	if c.frozen {
		return NewFrozenError("Set")
	}
	if c.closed {
		return NewClosedError("Set")
	}

	so := setOpts{allowOverride: true}
	for _, o := range opts {
		o(&so)
	}

	if !so.allowOverride && dictutil.HasNested(c.envConfig, keyPath) {
		return fmt.Errorf("key %q already exists (override=false)", keyPath)
	}

	// Defensively deep-copy any map/slice value so
	// later caller mutation does not alias into Config state. Scalars
	// pass through unchanged.
	stored := dictutil.DeepCopyValue(effectiveStored)

	// Structural snapshots preserve scalar types, nil leaves, and sub-tree
	// shape. SetNested may insert intermediate maps before returning a
	// PathError, so every failure restores envConfig as well.
	envSnap := dictutil.DeepCopyValue(c.envConfig).(map[string]any)
	unresolvedEnvSnap := dictutil.DeepCopyValue(c.unresolvedEnvConfig).(map[string]any)
	mergedSnap := dictutil.DeepCopyValue(c.mergedConfig).(map[string]any)
	trackerSnap := c.sourceTracker.Snapshot()

	// Snapshot pre-mutation flat state under the write lock.
	// Used downstream for the change-notification payload.
	oldFlat := dictutil.Flatten(c.envConfig)

	// Try the mutation against envConfig first. SetNested returns a
	// *PathError when a key path traverses through a non-map; surface
	// that as a typed *ConfigError and do NOT mutate mergedConfig.
	if err := dictutil.SetNested(c.envConfig, keyPath, stored); err != nil {
		// SetNested may have
		// inserted intermediate maps before failing, so restore envConfig
		// from the deep-copy snapshot. mergedConfig is untouched on this
		// path but we restore it (and the tracker) for symmetry with the
		// second-call failure path below.
		c.envConfig = envSnap
		c.unresolvedEnvConfig = unresolvedEnvSnap
		c.mergedConfig = mergedSnap
		c.sourceTracker.Restore(trackerSnap)
		if c.observer != nil {
			c.observer.RecordSetFailed()
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("set_failed", keyPath, err)
		}
		return NewInvalidError("Set", keyPath, err)
	}
	if err := dictutil.SetNested(c.unresolvedEnvConfig, keyPath, rawStored); err != nil {
		c.envConfig = envSnap
		c.unresolvedEnvConfig = unresolvedEnvSnap
		c.mergedConfig = mergedSnap
		c.sourceTracker.Restore(trackerSnap)
		if c.observer != nil {
			c.observer.RecordSetFailed()
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("set_failed", keyPath, err)
		}
		return NewInvalidError("Set", keyPath, err)
	}
	// mergedConfig must agree with envConfig; the same path-error guard
	// applies. If this fails, roll back the envConfig mutation so the
	// two maps stay in lockstep.
	if err := dictutil.SetNested(c.mergedConfig, keyPath, rawStored); err != nil {
		// Restore envConfig
		// AND mergedConfig from their deep-copy snapshots to preserve
		// scalar type identity (int stays int, time.Duration stays
		// Duration), nil-leaf distinctions, and sub-tree shape. The
		// tracker snapshot is restored even though TrackValue has not yet fired.
		c.envConfig = envSnap
		c.unresolvedEnvConfig = unresolvedEnvSnap
		c.mergedConfig = mergedSnap
		c.sourceTracker.Restore(trackerSnap)
		if c.observer != nil {
			c.observer.RecordSetFailed()
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("set_failed", keyPath, err)
		}
		return NewInvalidError("Set", keyPath, err)
	}
	if err := c.validateMaterializedCandidate(c.envConfig); err != nil {
		c.envConfig = envSnap
		c.unresolvedEnvConfig = unresolvedEnvSnap
		c.mergedConfig = mergedSnap
		c.sourceTracker.Restore(trackerSnap)
		if c.observer != nil {
			c.observer.RecordSetFailed()
		}
		if c.eventEmitter != nil {
			c.eventEmitter.Emit("set_failed", keyPath, err)
		}
		return err
	}

	// Source-tracking parity: a successful Set claims "runtime" as the
	// source so Explain/GetSourceInfo report the runtime origin until a
	// subsequent Reload overwrites it.
	c.sourceTracker.TrackValue(keyPath, rawStored, "runtime", "runtime", c.env)

	c.validatedModel = nil
	c.revision++

	// Snapshot everything callbacks/observability need WHILE the write
	// lock is held, then release the lock BEFORE invoking them.
	callbacks := c.snapshotChangeCallbacks()
	contextCallbacks := c.snapshotChangeContextCallbacks()
	newFlat := dictutil.Flatten(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)
	c.notifyContextChangesUnlocked(ctx, contextCallbacks, oldFlat, newFlat)

	if observer != nil {
		observer.RecordSet()
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "set", keyPath, stored)
		emitter.EmitWithContext(ctx, "change", keyPath, stored)
	}

	return nil
}

// OnChange registers fn to observe committed configuration changes. A nil fn
// is ignored. The callback is invoked once for each changed leaf key produced
// by:
//   - [Config.Set]
//   - [Config.Reload]
//   - [Config.Extend]
//   - [Config.Override]
//   - the restore closure returned by [Config.Override]
//   - [Config.RefreshSecrets]
//
// The payload uses (oldValue, newValue): replacements provide both values,
// additions provide nil as oldValue, and removals provide nil as newValue.
// Explicit nil values cannot be distinguished from additions or removals by
// this callback alone. Key delivery order is unspecified; callbacks for a key
// run in registration order.
//
// Callbacks run after the change is committed and without holding the Config
// lock, so they may call Config methods. The payload is a stable description
// of that commit, while a concurrent read may already observe a newer commit.
// A mutation performed by a callback produces another notification. Callers
// that derive configuration inside a callback must therefore prevent
// unbounded recursion. For example:
//
//	var fired atomic.Bool
//	cfg.OnChange(func(key string, oldVal, newVal any) {
//		if key == "source.value" && fired.CompareAndSwap(false, true) {
//			_ = cfg.Set("derived.value", deriveFrom(newVal))
//		}
//	})
//
// A callback panic is recovered and logged; remaining callbacks continue.
func (c *Config[T]) OnChange(fn func(key string, oldVal, newVal any)) {
	if fn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.changeCallbacks = append(c.changeCallbacks, fn)
}

// OnChangeWithContext is the context-aware form of [Config.OnChange]. The
// callback receives the context supplied to the mutation, allowing callers to
// correlate changes with traces and request metadata. It otherwise follows
// the same delivery, reentrancy, ordering, and panic-recovery contract. A nil
// callback is ignored.
func (c *Config[T]) OnChangeWithContext(fn func(context.Context, string, any, any)) {
	if fn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.changeContextCallbacks = append(c.changeContextCallbacks, fn)
}

func (c *Config[T]) snapshotChangeContextCallbacks() []func(context.Context, string, any, any) {
	return append([]func(context.Context, string, any, any){}, c.changeContextCallbacks...)
}

func (c *Config[T]) notifyContextChangesUnlocked(ctx context.Context, callbacks []func(context.Context, string, any, any), oldFlat, newFlat map[string]any) {
	if len(callbacks) == 0 {
		return
	}
	c.notifyChangeSet(oldFlat, newFlat, func(key string, oldVal, newVal any) {
		for index, callback := range callbacks {
			func() {
				defer func() {
					if recovered := recover(); recovered != nil && c.logger != nil {
						c.logger.Error("OnChangeWithContext callback panic recovered", slog.String("key", key), slog.Int("callback_index", index), slog.Any("panic", recovered), slog.String("stack", string(debug.Stack())))
					}
				}()
				callback(ctx, key, oldVal, newVal)
			}()
		}
	})
}

func (c *Config[T]) notifyChangeSet(oldFlat, newFlat map[string]any, emit func(string, any, any)) {
	seen := make(map[string]struct{}, len(oldFlat)+len(newFlat))
	for key := range oldFlat {
		seen[key] = struct{}{}
	}
	for key := range newFlat {
		seen[key] = struct{}{}
	}
	for key := range seen {
		oldVal, hadOld := oldFlat[key]
		newVal, hasNew := newFlat[key]
		if hadOld && hasNew && reflect.DeepEqual(oldVal, newVal) {
			continue
		}
		if !hadOld {
			oldVal = nil
		}
		if !hasNew {
			newVal = nil
		}
		emit(key, oldVal, newVal)
	}
}

// snapshotChangeCallbacks returns a defensive copy of the registered
// change callbacks. Callers MUST hold c.mu (read or write) when invoking
// this helper. The returned slice is owned by the caller and may be
// iterated outside the lock without racing against concurrent OnChange
// registrations.
func (c *Config[T]) snapshotChangeCallbacks() []func(key string, oldVal, newVal any) {
	if len(c.changeCallbacks) == 0 {
		return nil
	}
	out := make([]func(key string, oldVal, newVal any), len(c.changeCallbacks))
	copy(out, c.changeCallbacks)
	return out
}

// notifyChangesUnlocked fires the supplied callbacks for every key that
// differs between oldFlat and newFlat. It MUST be called with c.mu
// released so that callbacks may freely call back into the public API
// (Get/Set/Reload/etc.) without deadlocking on c.mu.
//
// Deletion semantics: this iterates the union of key sets, so:
//   - keys present in both with different values fire (oldVal, newVal).
//   - keys present in old only fire (oldVal, nil) — the deletion case.
//   - keys present in new only fire (nil, newVal) — the addition case.
//   - keys present in both with equal values are skipped.
//
// Callback panics are recovered per-callback so a single misbehaving
// listener cannot prevent peers from observing the change.
func (c *Config[T]) notifyChangesUnlocked(
	callbacks []func(key string, oldVal, newVal any),
	oldFlat, newFlat map[string]any,
) {
	if len(callbacks) == 0 {
		return
	}
	// Iterate the union of keys so removed keys (present in oldFlat,
	// absent from newFlat) and newly introduced keys (present in
	// newFlat, absent from oldFlat) are both reported.
	seen := make(map[string]struct{}, len(oldFlat)+len(newFlat))
	for key := range oldFlat {
		seen[key] = struct{}{}
	}
	for key := range newFlat {
		seen[key] = struct{}{}
	}
	for key := range seen {
		oldVal, hadOld := oldFlat[key]
		newVal, hasNew := newFlat[key]
		// Suppress only when both sides are present AND structurally
		// equal under reflect.DeepEqual.
		//
		// reflect.DeepEqual is type-aware: int(8080) and float64(8080)
		// are not equal, so a JSON-decoded float64 replacing an int
		// correctly fires the callback for downstream re-coercion.
		if hadOld && hasNew && reflect.DeepEqual(oldVal, newVal) {
			continue
		}
		// Removed keys surface (oldVal, nil); added keys surface
		// (nil, newVal) — the deletion contract.
		var emitOld, emitNew any
		if hadOld {
			emitOld = oldVal
		}
		if hasNew {
			emitNew = newVal
		}
		for i, cb := range callbacks {
			c.invokeChangeCallback(cb, i, key, emitOld, emitNew)
		}
	}
}

// invokeChangeCallback runs cb under a recover guard. A panicking
// callback is logged at error level on c.logger with the affected
// key, the callback index, the recovered value, and a runtime stack
// trace. Sibling callbacks continue regardless.
func (c *Config[T]) invokeChangeCallback(
	cb func(key string, oldVal, newVal any),
	callbackIndex int,
	key string,
	oldVal, newVal any,
) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if c.logger != nil {
			c.logger.Error(
				"OnChange callback panic recovered",
				slog.String("key", key),
				slog.Int("callback_index", callbackIndex),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())),
			)
		}
	}()
	cb(key, oldVal, newVal)
}
