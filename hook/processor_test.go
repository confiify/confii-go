// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessor_ContextHookRegistrationAndOrder(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-42")
	p := NewProcessor()

	appendStage := func(stage string) FuncCtx {
		return func(gotCtx context.Context, _ string, value any) (any, error) {
			assert.Equal(t, "request-42", gotCtx.Value(contextKey{}))
			return value.(string) + stage, nil
		}
	}

	p.RegisterKeyHookCtx("key", appendStage("-key"))
	p.RegisterValueHookCtx("start", appendStage("-value"))
	p.RegisterConditionHookCtx(
		func(key string, _ any) bool { return key == "key" },
		appendStage("-condition"),
	)
	p.RegisterGlobalHookCtx(appendStage("-global"))

	got, err := p.ProcessCtx(ctx, "key", "start")
	assert.NoError(t, err)
	assert.Equal(t, "start-key-value-condition-global", got)
}

func TestProcessor_GlobalHook(t *testing.T) {
	p := NewProcessor()
	p.RegisterGlobalHook(func(_ string, v any) any {
		if s, ok := v.(string); ok {
			return strings.ToUpper(s)
		}
		return v
	})

	result := p.Process("key", "hello")
	assert.Equal(t, "HELLO", result)
}

func TestProcessor_KeyHook(t *testing.T) {
	p := NewProcessor()
	p.RegisterKeyHook("database.host", func(_ string, _ any) any {
		return "overridden"
	})

	assert.Equal(t, "overridden", p.Process("database.host", "localhost"))
	assert.Equal(t, "localhost", p.Process("other.key", "localhost"))
}

func TestProcessor_ValueHook(t *testing.T) {
	p := NewProcessor()
	p.RegisterValueHook("secret_placeholder", func(_ string, _ any) any {
		return "resolved_secret"
	})

	assert.Equal(t, "resolved_secret", p.Process("key", "secret_placeholder"))
	assert.Equal(t, "other", p.Process("key", "other"))
}

func TestProcessor_ConditionHook(t *testing.T) {
	p := NewProcessor()
	p.RegisterConditionHook(
		func(_ string, v any) bool {
			s, ok := v.(string)
			return ok && strings.HasPrefix(s, "env:")
		},
		func(_ string, v any) any {
			return strings.TrimPrefix(v.(string), "env:")
		},
	)

	assert.Equal(t, "VALUE", p.Process("key", "env:VALUE"))
	assert.Equal(t, "plain", p.Process("key", "plain"))
}

func TestProcessor_ExecutionOrder(t *testing.T) {
	p := NewProcessor()
	var order []string

	p.RegisterKeyHook("k", func(_ string, v any) any {
		order = append(order, "key")
		return v
	})
	p.RegisterValueHook("val", func(_ string, v any) any {
		order = append(order, "value")
		return v
	})
	p.RegisterConditionHook(
		func(_ string, _ any) bool { return true },
		func(_ string, v any) any {
			order = append(order, "condition")
			return v
		},
	)
	p.RegisterGlobalHook(func(_ string, v any) any {
		order = append(order, "global")
		return v
	})

	p.Process("k", "val")
	assert.Equal(t, []string{"key", "value", "condition", "global"}, order)
}

func TestProcessor_ConcurrentSafety(t *testing.T) {
	p := NewProcessor()
	p.RegisterGlobalHook(func(_ string, v any) any { return v })

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.Process("key", "value")
		}()
		go func() {
			defer wg.Done()
			p.RegisterGlobalHook(func(_ string, v any) any { return v })
		}()
	}
	wg.Wait()
}

func TestProcessor_ContextErrorsStopEachStage(t *testing.T) {
	boom := errors.New("hook failed")
	tests := []struct {
		name     string
		register func(*Processor)
	}{
		{"key", func(p *Processor) {
			p.RegisterKeyHookCtx("key", func(context.Context, string, any) (any, error) { return "key", boom })
		}},
		{"value", func(p *Processor) {
			p.RegisterValueHookCtx("value", func(context.Context, string, any) (any, error) { return "value", boom })
		}},
		{"condition", func(p *Processor) {
			p.RegisterConditionHookCtx(func(string, any) bool { return true }, func(context.Context, string, any) (any, error) { return "condition", boom })
		}},
		{"global", func(p *Processor) {
			p.RegisterGlobalHookCtx(func(context.Context, string, any) (any, error) { return "global", boom })
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProcessor()
			tc.register(p)
			got, err := p.ProcessCtx(context.Background(), "key", "value")
			assert.ErrorIs(t, err, boom)
			assert.Equal(t, tc.name, got)
		})
	}
}

func TestProcessor_NonComparableValueAndEmptyEntry(t *testing.T) {
	p := NewProcessor()
	p.valueHooks["unused"] = []hookEntry{{}}
	p.globalHooks = []hookEntry{{}}
	value := []string{"not", "comparable"}
	got, err := p.ProcessCtx(context.Background(), "key", value)
	assert.NoError(t, err)
	assert.Equal(t, value, got)

	got, err = (hookEntry{}).run(context.Background(), "key", "value")
	assert.NoError(t, err)
	assert.Equal(t, "value", got)
}
