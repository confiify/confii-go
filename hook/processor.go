// Package hook provides a thread-safe hook processor for transforming
// configuration values during access.
package hook

import (
	"context"
	"sync"
)

// Func transforms a configuration value during access.
// It receives the full dot-separated key path and the current value,
// and returns the transformed value.
//
// Func is the original, context-free hook signature retained for backward
// compatibility. New hooks that require request-scoped context (deadlines,
// cancellation, caller values) or that need to surface resolution errors to
// the caller should implement [FuncCtx] instead and register via
// [Processor.RegisterGlobalHookCtx] (and friends).
type Func func(key string, value any) any

// FuncCtx is a context-aware hook that may return an error.
//
// Unlike [Func], a FuncCtx receives the caller's context (so it can honor
// per-request deadlines, cancellation, and propagated values) and may return
// an error which the [Processor] surfaces back to the caller of
// [Processor.ProcessCtx]. Hooks that perform I/O (secret resolution, remote
// lookups) should prefer this signature so the caller can fail fast.
type FuncCtx func(ctx context.Context, key string, value any) (any, error)

// Condition determines whether a conditional hook should fire.
type Condition func(key string, value any) bool

// Processor manages hook registration and execution.
// It is safe for concurrent use.
type Processor struct {
	mu             sync.RWMutex
	keyHooks       map[string][]hookEntry
	valueHooks     map[any][]hookEntry
	conditionHooks []conditionEntry
	globalHooks    []hookEntry
}

// hookEntry stores a hook in its native form. Exactly one of fn / fnCtx is
// non-nil. Storing the native form (rather than always wrapping legacy hooks
// into FuncCtx) keeps ProcessCtx error-aware while still allowing Process to
// run legacy hooks unchanged.
type hookEntry struct {
	fn    Func
	fnCtx FuncCtx
}

type conditionEntry struct {
	cond  Condition
	entry hookEntry
}

// NewProcessor creates a new hook processor.
func NewProcessor() *Processor {
	return &Processor{
		keyHooks:   make(map[string][]hookEntry),
		valueHooks: make(map[any][]hookEntry),
	}
}

// RegisterKeyHook registers a hook that fires when the key exactly matches.
func (p *Processor) RegisterKeyHook(key string, h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keyHooks[key] = append(p.keyHooks[key], hookEntry{fn: h})
}

// RegisterKeyHookCtx registers a context-aware hook that fires when the key
// exactly matches. Errors returned by the hook propagate back through
// [Processor.ProcessCtx].
func (p *Processor) RegisterKeyHookCtx(key string, h FuncCtx) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keyHooks[key] = append(p.keyHooks[key], hookEntry{fnCtx: h})
}

// RegisterValueHook registers a hook that fires when the value exactly matches.
// Only works for comparable (hashable) values.
func (p *Processor) RegisterValueHook(value any, h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.valueHooks[value] = append(p.valueHooks[value], hookEntry{fn: h})
}

// RegisterValueHookCtx registers a context-aware hook that fires when the
// value exactly matches. Only works for comparable (hashable) values.
func (p *Processor) RegisterValueHookCtx(value any, h FuncCtx) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.valueHooks[value] = append(p.valueHooks[value], hookEntry{fnCtx: h})
}

// RegisterConditionHook registers a hook that fires when the condition returns true.
func (p *Processor) RegisterConditionHook(cond Condition, h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conditionHooks = append(p.conditionHooks, conditionEntry{cond: cond, entry: hookEntry{fn: h}})
}

// RegisterConditionHookCtx registers a context-aware hook that fires when the
// condition returns true.
func (p *Processor) RegisterConditionHookCtx(cond Condition, h FuncCtx) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conditionHooks = append(p.conditionHooks, conditionEntry{cond: cond, entry: hookEntry{fnCtx: h}})
}

// RegisterGlobalHook registers a hook that fires for every value.
func (p *Processor) RegisterGlobalHook(h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.globalHooks = append(p.globalHooks, hookEntry{fn: h})
}

// RegisterGlobalHookCtx registers a context-aware hook that fires for every
// value. Errors returned by the hook propagate back through
// [Processor.ProcessCtx].
func (p *Processor) RegisterGlobalHookCtx(h FuncCtx) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.globalHooks = append(p.globalHooks, hookEntry{fnCtx: h})
}

// Process applies all applicable hooks to the value in the defined order:
// key hooks → value hooks → condition hooks → global hooks.
// Each hook's output becomes the next hook's input.
//
// Process is the legacy entry point and uses [context.Background] when
// invoking context-aware hooks. Errors returned by context-aware hooks are
// silently discarded; callers that need to observe such errors must use
// [Processor.ProcessCtx].
func (p *Processor) Process(key string, value any) any {
	v, _ := p.ProcessCtx(context.Background(), key, value)
	return v
}

// ProcessCtx is the context-aware variant of [Processor.Process]. It threads
// ctx through any registered [FuncCtx] hooks and returns the first error
// produced by such a hook (after which subsequent hooks are skipped). Legacy
// [Func] hooks remain non-error-returning and run as before.
func (p *Processor) ProcessCtx(ctx context.Context, key string, value any) (any, error) {
	// Snapshot hook slices under read lock to avoid holding lock during execution.
	p.mu.RLock()
	keyH := append([]hookEntry(nil), p.keyHooks[key]...)
	var valueH []hookEntry
	if isComparable(value) {
		valueH = append([]hookEntry(nil), p.valueHooks[value]...)
	}
	condH := append([]conditionEntry(nil), p.conditionHooks...)
	globalH := append([]hookEntry(nil), p.globalHooks...)
	p.mu.RUnlock()

	for _, e := range keyH {
		v, err := e.run(ctx, key, value)
		if err != nil {
			return v, err
		}
		value = v
	}
	for _, e := range valueH {
		v, err := e.run(ctx, key, value)
		if err != nil {
			return v, err
		}
		value = v
	}
	for _, ce := range condH {
		if ce.cond(key, value) {
			v, err := ce.entry.run(ctx, key, value)
			if err != nil {
				return v, err
			}
			value = v
		}
	}
	for _, e := range globalH {
		v, err := e.run(ctx, key, value)
		if err != nil {
			return v, err
		}
		value = v
	}

	return value, nil
}

// run executes the entry, dispatching to whichever variant is populated.
func (e hookEntry) run(ctx context.Context, key string, value any) (any, error) {
	if e.fnCtx != nil {
		return e.fnCtx(ctx, key, value)
	}
	if e.fn != nil {
		return e.fn(key, value), nil
	}
	return value, nil
}

// isComparable checks if a value can be used as a map key.
func isComparable(v any) bool {
	defer func() { _ = recover() }()
	_ = v == v
	return true
}
