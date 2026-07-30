// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"context"
	"log/slog"
	"sync"
)

// EventEmitter synchronously dispatches named events to registered listeners.
// Registration and emission are safe for concurrent use. Listener execution
// occurs in registration order on the emitting goroutine.
type EventEmitter struct {
	mu               sync.RWMutex
	listeners        map[string][]func(args ...any)
	contextListeners map[string][]func(context.Context, ...any)
	logger           *slog.Logger
}

// NewEventEmitter creates an empty emitter. A nil logger selects
// [slog.Default].
func NewEventEmitter(logger *slog.Logger) *EventEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventEmitter{
		listeners:        make(map[string][]func(args ...any)),
		contextListeners: make(map[string][]func(context.Context, ...any)),
		logger:           logger,
	}
}

// OnWithContext appends a context-aware listener for event and returns the
// emitter for chaining. A nil listener is not supported and will panic when
// the event is emitted.
func (e *EventEmitter) OnWithContext(event string, fn func(context.Context, ...any)) *EventEmitter {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.contextListeners[event] = append(e.contextListeners[event], fn)
	return e
}

// On appends a listener for event and returns the emitter for chaining. A nil
// listener is not supported and will panic when the event is emitted.
func (e *EventEmitter) On(event string, fn func(args ...any)) *EventEmitter {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners[event] = append(e.listeners[event], fn)
	return e
}

// Off removes the most recently registered context-free listener for event. It
// is a no-op when none is registered.
func (e *EventEmitter) Off(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if fns, ok := e.listeners[event]; ok && len(fns) > 0 {
		e.listeners[event] = fns[:len(fns)-1]
	}
}

// OffWithContext removes the most recently registered context-aware listener
// for event. It is a no-op when none is registered.
func (e *EventEmitter) OffWithContext(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if fns := e.contextListeners[event]; len(fns) > 0 {
		e.contextListeners[event] = fns[:len(fns)-1]
	}
}

// Emit dispatches event with a background context. Listener panics are
// recovered and logged, and remaining listeners continue.
func (e *EventEmitter) Emit(event string, args ...any) {
	e.EmitWithContext(context.Background(), event, args...)
}

// EmitWithContext dispatches context-free listeners first, followed by
// context-aware listeners, each in registration order. ctx is passed through
// without interpretation and may be nil. Listener panics are recovered and
// logged; EmitWithContext does not return listener errors.
func (e *EventEmitter) EmitWithContext(ctx context.Context, event string, args ...any) {
	e.mu.RLock()
	fns := make([]func(args ...any), len(e.listeners[event]))
	copy(fns, e.listeners[event])
	contextFns := append([]func(context.Context, ...any){}, e.contextListeners[event]...)
	e.mu.RUnlock()

	for _, fn := range fns {
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("event listener panic",
						slog.String("event", event),
						slog.Any("panic", r),
					)
				}
			}()
			fn(args...)
		}()
	}
	for _, fn := range contextFns {
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("context event listener panic", slog.String("event", event), slog.Any("panic", r))
				}
			}()
			fn(ctx, args...)
		}()
	}
}
