package observe

import (
	"log/slog"
	"sync"
)

// EventEmitter dispatches named events to registered listeners.
type EventEmitter struct {
	mu        sync.RWMutex
	listeners map[string][]func(args ...any)
	logger    *slog.Logger
}

// NewEventEmitter creates a new event emitter.
func NewEventEmitter(logger *slog.Logger) *EventEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &EventEmitter{
		listeners: make(map[string][]func(args ...any)),
		logger:    logger,
	}
}

// On registers a listener for the given event type. Returns the emitter for chaining.
func (e *EventEmitter) On(event string, fn func(args ...any)) *EventEmitter {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners[event] = append(e.listeners[event], fn)
	return e
}

// Off removes a specific listener. Since Go funcs aren't comparable,
// this removes the last registered listener for the event.
func (e *EventEmitter) Off(event string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if fns, ok := e.listeners[event]; ok && len(fns) > 0 {
		e.listeners[event] = fns[:len(fns)-1]
	}
}

// Emit fires all listeners for the given event.
// Exceptions in listeners are caught and logged, not propagated.
func (e *EventEmitter) Emit(event string, args ...any) {
	e.mu.RLock()
	fns := make([]func(args ...any), len(e.listeners[event]))
	copy(fns, e.listeners[event])
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
}
