package observe

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventEmitter_EmitNoListeners(t *testing.T) {
	e := NewEventEmitter(nil)
	// Emitting an event with no listeners should not panic.
	e.Emit("nonexistent", "arg1", "arg2")
}

func TestEventEmitter_OffWithNoListeners(t *testing.T) {
	e := NewEventEmitter(nil)
	// Off on a non-existent event should not panic.
	e.Off("nonexistent")
}

func TestEventEmitter_OffRemovesOnlyLast(t *testing.T) {
	e := NewEventEmitter(nil)
	var calls []string

	e.On("evt", func(_ ...any) { calls = append(calls, "first") })
	e.On("evt", func(_ ...any) { calls = append(calls, "second") })
	e.On("evt", func(_ ...any) { calls = append(calls, "third") })

	e.Off("evt") // removes "third"
	e.Emit("evt")
	assert.Equal(t, []string{"first", "second"}, calls)
}

func TestEventEmitter_Chaining(t *testing.T) {
	e := NewEventEmitter(nil)
	var count int

	// On returns the emitter for chaining.
	e.On("evt", func(_ ...any) { count++ }).
		On("evt", func(_ ...any) { count++ })

	e.Emit("evt")
	assert.Equal(t, 2, count)
}

func TestEventEmitter_PanicDoesNotAffectOtherListeners(t *testing.T) {
	e := NewEventEmitter(nil)
	var results []string

	e.On("evt", func(_ ...any) { results = append(results, "before") })
	e.On("evt", func(_ ...any) { panic("boom") })
	e.On("evt", func(_ ...any) { results = append(results, "after") })

	e.Emit("evt")
	assert.Equal(t, []string{"before", "after"}, results)
}

func TestEventEmitter_EmitWithArgs(t *testing.T) {
	e := NewEventEmitter(nil)
	var received []any

	e.On("data", func(args ...any) {
		received = args
	})

	e.Emit("data", 42, "hello", true)
	assert.Equal(t, []any{42, "hello", true}, received)
}

func TestEventEmitter_EmitNoArgs(t *testing.T) {
	e := NewEventEmitter(nil)
	called := false

	e.On("ping", func(args ...any) {
		called = true
		assert.Empty(t, args)
	})

	e.Emit("ping")
	assert.True(t, called)
}

func TestEventEmitter_MultipleEvents(t *testing.T) {
	e := NewEventEmitter(nil)
	var eventA, eventB int

	e.On("a", func(_ ...any) { eventA++ })
	e.On("b", func(_ ...any) { eventB++ })

	e.Emit("a")
	e.Emit("a")
	e.Emit("b")

	assert.Equal(t, 2, eventA)
	assert.Equal(t, 1, eventB)
}
