package observe

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventEmitter_OnAndEmit(t *testing.T) {
	e := NewEventEmitter(nil)
	var received []any

	e.On("test", func(args ...any) {
		received = append(received, args...)
	})

	e.Emit("test", "hello", 42)
	assert.Equal(t, []any{"hello", 42}, received)
}

func TestEventEmitter_MultipleListeners(t *testing.T) {
	e := NewEventEmitter(nil)
	var count int
	var mu sync.Mutex

	for range 3 {
		e.On("event", func(_ ...any) {
			mu.Lock()
			count++
			mu.Unlock()
		})
	}

	e.Emit("event")
	assert.Equal(t, 3, count)
}

func TestEventEmitter_PanicRecovery(t *testing.T) {
	e := NewEventEmitter(nil)
	var recovered bool

	e.On("crash", func(_ ...any) {
		panic("boom")
	})
	e.On("crash", func(_ ...any) {
		recovered = true
	})

	e.Emit("crash") // should not panic
	assert.True(t, recovered)
}

func TestEventEmitter_Off(t *testing.T) {
	e := NewEventEmitter(nil)
	count := 0
	e.On("event", func(_ ...any) { count++ })
	e.On("event", func(_ ...any) { count++ })

	e.Off("event") // remove last listener
	e.Emit("event")
	assert.Equal(t, 1, count) // only first listener fires
}
