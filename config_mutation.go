// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"

	"github.com/confiify/confii-go/internal/dictutil"
)

// SetOption is a functional option that configures the behavior of [Config.Set].
type SetOption func(*setOpts)

type setOpts struct{ allowOverride bool }

// WithOverride allows or prevents overwriting existing keys. Default: true.
func WithOverride(v bool) SetOption {
	return func(o *setOpts) { o.allowOverride = v }
}

// Set sets a value by dot-separated key path. Thread-safe, respects frozen state.
// Pass WithOverride(false) to raise an error if the key already exists.
//
// G12 contract (Wave 11):
//
//   - Source-tracking parity. A successful Set records the new value
//     against the synthetic source "runtime" so [Config.Explain] and
//     friends report runtime mutations alongside file/env-loaded keys.
//     The runtime tracking is overwritten by a subsequent Reload.
//
//   - Error propagation. dictutil.SetNested returns a *PathError when
//     a key path traverses through a non-map intermediate (for example
//     setting "service.host" when "service" is already bound to a
//     scalar). Pre-G12 this error was silently swallowed; Set now
//     surfaces it as a typed [*ConfigError] wrapping [ErrConfigInvalid]
//     and leaves both envConfig and mergedConfig unmutated.
//
//   - Defensive deep copy (F-G10-SetInputAlias). The caller's value is
//     deep-copied via [dictutil.DeepCopyValue] before storage so a
//     subsequent caller-side mutation of a passed map/slice cannot
//     bleed into Config state. This mirrors the read-side deep-copy
//     contract introduced in G11.
//
//   - Change-event parity (F-G12-Set-CallbackSilence). Following the
//     G13 lock-release-then-callback pattern (see [Config.Reload],
//     [Config.Extend], [Config.Override]), a successful Set fires
//     OnChange callbacks AFTER c.mu has been released so a callback
//     that calls back into the Config cannot deadlock. Callbacks see
//     the same union-iteration deletion contract: setting a key to a
//     new non-equal value emits (oldVal, newVal); a key that was
//     present and is now bound to a value that flatten cannot reach
//     (e.g. nil) emits (oldVal, nil).
//
//   - Observability parity (G21 residual). When metrics or events are
//     enabled, a successful Set emits "set" / "change" events and
//     [observe.Metrics.RecordSet] / [observe.Metrics.RecordChange];
//     a failed Set emits "set_failed" and [observe.Metrics.RecordSetFailed].
//
//   - Rollback fidelity (F-Set-RollbackFidelity, Wave 17). Pre-Wave 17
//     a SetNested *PathError on c.mergedConfig rolled envConfig back via
//     dictutil.Unflatten(Flatten(envConfig)), which collapses non-string
//     scalar types (int -> float64, time.Duration -> int64), drops
//     nil-leaf distinctions, and cannot reconstruct sub-tree shape. The
//     rollback is now a structural Snapshot+Restore of envConfig,
//     mergedConfig, and the source tracker — the same idiom used by
//     [Config.Reload] (Wave 7 G14) and [Config.Override] (Wave 16). The
//     pre-Wave 17 code also skipped rollback on the FIRST SetNested
//     failure even though that call may have inserted intermediate maps
//     before erroring; the Wave 17 path rolls envConfig back on both
//     SetNested failures so a rejected Set leaves no observable trace.
//
//   - fileTracker divergence is intentional (G12-NewKey-Loaders, Wave 19).
//     Set claims the synthetic source "runtime" via [sourcetrack.Tracker.TrackValue]
//     but does NOT register a corresponding entry in c.fileTracker — the
//     fileTracker is the file-mtime/hash gate consumed by [Config.Reload]'s
//     incremental "did anything change?" filter, and runtime keys are not
//     file-backed so they have no mtime/hash to track. The two trackers
//     therefore diverge by design: sourceTracker reports runtime origin
//     for introspection (Explain / GetSourceInfo / GetConflicts), while
//     fileTracker only watches loader-backed files for incremental change
//     detection. The divergence is observable but harmless:
//
//   - When Reload's gate short-circuits because no underlying file
//     changed, runtime-Set keys persist in envConfig/mergedConfig and
//     OnChange does not fire spuriously.
//
//   - When Reload's gate fires because a file changed, Reload rebuilds
//     envConfig from the merged file data (Phase 4), so runtime-only
//     keys are evicted and OnChange correctly emits (runtimeValue, nil)
//     under the union-iteration deletion contract.
//
//     Both behaviors are documented Reload semantics: runtime mutations
//     are intentionally non-persistent across Reload because Reload commits
//     a fresh file-loaded view. Callers that need a runtime override to
//     survive Reload should re-Set after Reload, or use a [Config.Override]
//     restore handle. Adding a fileTracker.RegisterRuntimeKey API was
//     considered and rejected — runtime keys cannot be watched via
//     mtime/hash and the existing Wave 11 G12 source-tracker entry already
//     provides correct introspection.
func (c *Config[T]) Set(keyPath string, value any, opts ...SetOption) error {
	// Resolve a private candidate before taking the Config lock. Hooks may
	// perform remote I/O or re-enter Config, and a successful Set must preserve
	// the invariant that all live reads observe an already-materialized value.
	rawStored := dictutil.DeepCopyValue(value)
	effectiveStored, materializeErr := c.materializeEffectiveValue(context.Background(), keyPath, rawStored)
	if materializeErr != nil {
		return &ConfigError{
			Op:  "Set",
			Err: fmt.Errorf("%w: materialize %q: %w", ErrConfigLoad, keyPath, materializeErr),
		}
	}
	c.mu.Lock()
	// G13: see Reload for the rationale behind the manual unlock flag.
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

	so := setOpts{allowOverride: true}
	for _, o := range opts {
		o(&so)
	}

	if !so.allowOverride && dictutil.HasNested(c.envConfig, keyPath) {
		return fmt.Errorf("key %q already exists (override=false)", keyPath)
	}

	// F-G10-SetInputAlias: defensively deep-copy any map/slice value so
	// later caller mutation does not alias into Config state. Scalars
	// pass through unchanged.
	stored := dictutil.DeepCopyValue(effectiveStored)

	// F-Set-RollbackFidelity (Wave 17): structural snapshot/restore.
	// Pre-Wave 17 the rollback path used dictutil.Unflatten(Flatten(envConfig)),
	// which is lossy along two axes: (a) a yaml/json round-trip via the
	// flat-keys representation collapses non-string scalar types
	// (int -> float64, time.Duration -> int64) and drops nil-leaf
	// distinctions; (b) on a hypothetical future sub-tree Set the flat
	// representation cannot reconstruct the original sub-tree shape /
	// key ordering. Adopting the same Snapshot+Restore idiom Wave 7 G14
	// (Reload) and Wave 16 (Override) use guarantees structural fidelity
	// of envConfig, mergedConfig, and the source tracker on rollback.
	// Note: dictutil.SetNested may insert intermediate maps before
	// returning a *PathError, so the FIRST SetNested call must also
	// roll back envConfig (the pre-Wave 17 code skipped this path).
	envSnap := dictutil.DeepCopyValue(c.envConfig).(map[string]any)
	unresolvedEnvSnap := dictutil.DeepCopyValue(c.unresolvedEnvConfig).(map[string]any)
	mergedSnap := dictutil.DeepCopyValue(c.mergedConfig).(map[string]any)
	trackerSnap := c.sourceTracker.Snapshot()

	// Snapshot pre-mutation flat state under the write lock (G13).
	// Used downstream for the change-notification payload.
	oldFlat := dictutil.Flatten(c.envConfig)

	// Try the mutation against envConfig first. SetNested returns a
	// *PathError when a key path traverses through a non-map; surface
	// that as a typed *ConfigError and do NOT mutate mergedConfig.
	if err := dictutil.SetNested(c.envConfig, keyPath, stored); err != nil {
		// Structural rollback (F-Set-RollbackFidelity): SetNested may have
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
		// Structural rollback (F-Set-RollbackFidelity): restore envConfig
		// AND mergedConfig from their deep-copy snapshots to preserve
		// scalar type identity (int stays int, time.Duration stays
		// Duration), nil-leaf distinctions, and sub-tree shape. The
		// tracker snapshot is restored for symmetry with Wave 16
		// Override even though TrackValue has not yet fired on this path.
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

	// Source-tracking parity: a successful Set claims "runtime" as the
	// source so Explain/GetSourceInfo report the runtime origin until a
	// subsequent Reload overwrites it.
	c.sourceTracker.TrackValue(keyPath, rawStored, "runtime", "runtime", c.env)

	c.validatedModel = nil

	// Snapshot everything callbacks/observability need WHILE the write
	// lock is held, then release the lock BEFORE invoking them (G13).
	callbacks := c.snapshotChangeCallbacks()
	newFlat := dictutil.Flatten(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)

	if observer != nil {
		observer.RecordSet()
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("set", keyPath, stored)
		emitter.Emit("change", keyPath, stored)
	}

	return nil
}

// OnChange registers a callback that fires whenever configuration
// values change.
//
// Firing surfaces. The callback is invoked from each of the following
// mutation entry points, once per key whose flattened value differs
// between the pre- and post-mutation snapshots:
//   - [Config.Set]                        (G12 / Wave 11)
//   - [Config.Reload]
//   - [Config.Extend]
//   - [Config.Override]
//   - the restore closure returned by [Config.Override]
//
// Lock-release contract (G13). Callbacks run AFTER the Config's internal
// write lock has been released. A callback may therefore freely call
// back into any public method on the Config — Get, Set, Has, Reload,
// Extend, Override — without deadlocking. The trade-off is that the
// Config state visible to a callback is whatever any concurrent
// goroutine has produced by the time the callback runs; the (oldVal,
// newVal) payload, however, is taken from a snapshot captured at the
// moment of the change and is stable regardless of subsequent mutations.
//
// Self-firing recursion (Wave 11 G12 / Wave 18). Because Set, Override,
// Reload, Extend, and the Override restore closure all themselves fire
// OnChange callbacks, a callback that mutates the Config will TRIGGER
// another OnChange invocation for that nested mutation. This is a
// load-bearing feature — it is what lets a callback transparently
// notify downstream listeners — but it means callers are responsible
// for guarding against unbounded recursion. The canonical idiom is an
// [atomic.Bool] CAS one-shot, NOT [sync.Once]: a recursive Once.Do
// deadlocks (see the documented Once contract), whereas atomic.Bool
// CAS lets the first entry into the callback win the right to mutate
// and subsequent self-fires become cheap no-ops. See
// TestOnChange_GodocContract_Recursion in config_callbacks_test.go for
// a runnable example. A typical guard reads:
//
//	var fired atomic.Bool
//	cfg.OnChange(func(key string, oldVal, newVal any) {
//	    if fired.CompareAndSwap(false, true) {
//	        _ = cfg.Set("derived.key", deriveFrom(newVal))
//	    }
//	})
//
// Diff-based firing (G13). The callback fires for every key whose
// pre/post flattened value differs. The (oldVal, newVal) payload uses
// untyped `any`:
//   - replaced key:   fn(key, oldVal, newVal) — both non-nil.
//   - removed key:    fn(key, oldVal, nil)    — newVal is the zero value.
//   - introduced key: fn(key, nil, newVal)    — oldVal is the zero value.
//
// Callbacks that need to distinguish "explicitly set to nil" from
// "removed" must compare against a sentinel themselves; the contract
// only guarantees that removal surfaces the zero `any`.
//
// Panic recovery (G13). A panicking callback does NOT abort sibling
// callbacks, nor does it abort the calling goroutine: each callback
// runs inside an independent recover() guard. A panic is logged via
// the Config's slog logger and the next callback in the registration
// order continues normally.
func (c *Config[T]) OnChange(fn func(key string, oldVal, newVal any)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.changeCallbacks = append(c.changeCallbacks, fn)
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
// G13 deletion semantics: this iterates the UNION of key sets, so:
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
