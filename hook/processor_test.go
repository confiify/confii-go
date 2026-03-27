package hook

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
