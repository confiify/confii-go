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
	"github.com/stretchr/testify/require"
)

func passthrough(_ context.Context, _ string, value any) (any, error) {
	return value, nil
}

func TestProcessor_ContextAndExecutionOrder(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-42")
	p := NewProcessor()
	appendStage := func(stage string) Func {
		return func(gotCtx context.Context, _ string, value any) (any, error) {
			assert.Equal(t, "request-42", gotCtx.Value(contextKey{}))
			return value.(string) + stage, nil
		}
	}
	p.RegisterKeyHook("key", appendStage("-key"))
	p.RegisterValueHook("start-key", appendStage("-value"))
	p.RegisterConditionHook(func(_ context.Context, key string, _ any) (bool, error) { return key == "key", nil },
		appendStage("-condition"),
	)
	p.RegisterGlobalHook(appendStage("-global"))

	got, err := p.Process(ctx, "key", "start")
	require.NoError(t, err)
	assert.Equal(t, "start-key-value-condition-global", got)
}

func TestProcessor_HookKinds(t *testing.T) {
	p := NewProcessor()
	p.RegisterKeyHook("database.host", func(_ context.Context, _ string, _ any) (any, error) {
		return "overridden", nil
	})
	p.RegisterValueHook("secret_placeholder", func(_ context.Context, _ string, _ any) (any, error) {
		return "resolved_secret", nil
	})
	p.RegisterConditionHook(func(_ context.Context, _ string, value any) (bool, error) {
		s, ok := value.(string)
		return ok && strings.HasPrefix(s, "env:"), nil
	},
		func(_ context.Context, _ string, value any) (any, error) {
			return strings.TrimPrefix(value.(string), "env:"), nil
		},
	)
	p.RegisterGlobalHook(func(_ context.Context, _ string, value any) (any, error) {
		if s, ok := value.(string); ok {
			return strings.ToUpper(s), nil
		}
		return value, nil
	})

	got, err := p.Process(context.Background(), "database.host", "localhost")
	require.NoError(t, err)
	assert.Equal(t, "OVERRIDDEN", got)
	got, err = p.Process(context.Background(), "other", "secret_placeholder")
	require.NoError(t, err)
	assert.Equal(t, "RESOLVED_SECRET", got)
	got, err = p.Process(context.Background(), "other", "env:value")
	require.NoError(t, err)
	assert.Equal(t, "VALUE", got)
}

func TestProcessor_ErrorsStopEveryStage(t *testing.T) {
	boom := errors.New("hook failed")
	tests := []struct {
		name     string
		register func(*Processor)
	}{
		{"key", func(p *Processor) {
			p.RegisterKeyHook("key", func(context.Context, string, any) (any, error) { return "key", boom })
		}},
		{"value", func(p *Processor) {
			p.RegisterValueHook("value", func(context.Context, string, any) (any, error) { return "value", boom })
		}},
		{"condition", func(p *Processor) {
			p.RegisterConditionHook(func(context.Context, string, any) (bool, error) { return false, boom }, passthrough)
		}},
		{"global", func(p *Processor) {
			p.RegisterGlobalHook(func(context.Context, string, any) (any, error) { return "global", boom })
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProcessor()
			tc.register(p)
			_, err := p.Process(context.Background(), "key", "value")
			assert.ErrorIs(t, err, boom)
		})
	}
}

func TestProcessor_SnapshotIsImmutableAndRevisioned(t *testing.T) {
	p := NewProcessor()
	p.RegisterGlobalHook(func(_ context.Context, _ string, value any) (any, error) {
		return value.(string) + "-one", nil
	})
	plan := p.Snapshot()
	p.RegisterGlobalHook(func(_ context.Context, _ string, value any) (any, error) {
		return value.(string) + "-two", nil
	})

	oldValue, err := plan.Process(context.Background(), "key", "start")
	require.NoError(t, err)
	newValue, err := p.Process(context.Background(), "key", "start")
	require.NoError(t, err)
	assert.Equal(t, "start-one", oldValue)
	assert.Equal(t, "start-one-two", newValue)
	assert.Less(t, plan.Revision(), p.Revision())
}

func TestProcessor_ValueHookSupportsNonComparableValues(t *testing.T) {
	p := NewProcessor()
	p.RegisterValueHook([]any{"a", "b"}, func(_ context.Context, _ string, _ any) (any, error) {
		return "matched", nil
	})
	got, err := p.Process(context.Background(), "key", []any{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, "matched", got)
}

func TestProcessor_ConcurrentSafety(t *testing.T) {
	p := NewProcessor()
	p.RegisterGlobalHook(passthrough)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = p.Process(context.Background(), "key", "value") }()
		go func() { defer wg.Done(); p.RegisterGlobalHook(passthrough) }()
	}
	wg.Wait()
}

func TestProcessor_RejectsNilRegistrations(t *testing.T) {
	p := NewProcessor()
	assert.Panics(t, func() { p.RegisterKeyHook("key", nil) })
	assert.Panics(t, func() { p.RegisterValueHook("value", nil) })
	assert.Panics(t, func() { p.RegisterConditionHook(nil, passthrough) })
	assert.Panics(t, func() {
		p.RegisterConditionHook(func(context.Context, string, any) (bool, error) { return true, nil }, nil)
	})
	assert.Panics(t, func() { p.RegisterGlobalHook(nil) })
}
