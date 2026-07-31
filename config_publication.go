// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/observe"
)

// committedChange is an immutable delivery plan captured while c.mu protects
// a successful publication. It lets callbacks and synchronous event listeners
// run after the lock is released without observing mutable callback slices or
// aliased configuration maps.
type committedChange struct {
	before           map[string]any
	after            map[string]any
	callbacks        []func(string, any, any)
	contextCallbacks []func(context.Context, string, any, any)
	observer         *observe.Metrics
	emitter          *observe.EventEmitter
}

// captureCommittedChange must be called while c.mu is held for reading or
// writing. before and after are copied so event listeners cannot mutate a
// payload shared with live configuration state or another listener phase.
func (c *Config[T]) captureCommittedChange(before, after map[string]any) committedChange {
	return committedChange{
		before:           dictutil.DeepCopy(before),
		after:            dictutil.DeepCopy(after),
		callbacks:        c.snapshotChangeCallbacks(),
		contextCallbacks: c.snapshotChangeContextCallbacks(),
		observer:         c.observer,
		emitter:          c.eventEmitter,
	}
}

// deliverCommittedChange invokes per-key callbacks, records the optional
// operation-specific metric and event, then records and emits the generic
// configuration change. The caller must invoke it only after releasing c.mu.
func (c *Config[T]) deliverCommittedChange(
	ctx context.Context,
	change committedChange,
	recordOperation func(*observe.Metrics),
	operationEvent string,
	operationArgs ...any,
) {
	oldFlat := dictutil.Flatten(change.before)
	newFlat := dictutil.Flatten(change.after)
	c.notifyChangesUnlocked(change.callbacks, oldFlat, newFlat)
	c.notifyContextChangesUnlocked(ctx, change.contextCallbacks, oldFlat, newFlat)
	if change.observer != nil && recordOperation != nil {
		recordOperation(change.observer)
	}
	if change.emitter != nil && operationEvent != "" {
		change.emitter.EmitWithContext(ctx, operationEvent, operationArgs...)
	}
	if change.observer != nil {
		change.observer.RecordChange()
	}
	if change.emitter != nil {
		change.emitter.EmitWithContext(
			ctx,
			"change",
			dictutil.DeepCopy(change.before),
			dictutil.DeepCopy(change.after),
		)
	}
}
