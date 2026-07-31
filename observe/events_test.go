// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventEmitter_ContextListenerReceivesOperationContext(t *testing.T) {
	e := NewEventEmitter(nil)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "trace")
	var received any
	e.OnWithContext("reload", func(callbackCtx context.Context, _ ...any) {
		received = callbackCtx.Value(contextKey{})
	})
	e.EmitWithContext(ctx, "reload")
	assert.Equal(t, "trace", received)
}

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

	for i := 0; i < 3; i++ {
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

	e.Emit("crash")
	assert.True(t, recovered)
}

func TestEventEmitter_Off(t *testing.T) {
	e := NewEventEmitter(nil)
	count := 0
	e.On("event", func(_ ...any) { count++ })
	e.On("event", func(_ ...any) { count++ })

	e.Off("event")
	e.Emit("event")
	assert.Equal(t, 1, count)
}

func TestEventEmitter_OffWithContext(t *testing.T) {
	e := NewEventEmitter(nil)
	count := 0
	e.OnWithContext("event", func(context.Context, ...any) { count++ })
	e.OnWithContext("event", func(context.Context, ...any) { count++ })

	e.OffWithContext("event")
	e.EmitWithContext(context.Background(), "event")
	assert.Equal(t, 1, count)

	e.OffWithContext("event")
	e.OffWithContext("unknown")
	e.EmitWithContext(context.Background(), "event")
	assert.Equal(t, 1, count)
}
