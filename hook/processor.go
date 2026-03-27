// Package hook provides a thread-safe hook processor for transforming
// configuration values during access.
package hook

import "sync"

// Func transforms a configuration value during access.
// It receives the full dot-separated key path and the current value,
// and returns the transformed value.
type Func func(key string, value any) any

// Condition determines whether a conditional hook should fire.
type Condition func(key string, value any) bool

// Processor manages hook registration and execution.
// It is safe for concurrent use.
type Processor struct {
	mu             sync.RWMutex
	keyHooks       map[string][]Func
	valueHooks     map[any][]Func
	conditionHooks []conditionEntry
	globalHooks    []Func
}

type conditionEntry struct {
	cond Condition
	hook Func
}

// NewProcessor creates a new hook processor.
func NewProcessor() *Processor {
	return &Processor{
		keyHooks:   make(map[string][]Func),
		valueHooks: make(map[any][]Func),
	}
}

// RegisterKeyHook registers a hook that fires when the key exactly matches.
func (p *Processor) RegisterKeyHook(key string, h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keyHooks[key] = append(p.keyHooks[key], h)
}

// RegisterValueHook registers a hook that fires when the value exactly matches.
// Only works for comparable (hashable) values.
func (p *Processor) RegisterValueHook(value any, h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.valueHooks[value] = append(p.valueHooks[value], h)
}

// RegisterConditionHook registers a hook that fires when the condition returns true.
func (p *Processor) RegisterConditionHook(cond Condition, h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conditionHooks = append(p.conditionHooks, conditionEntry{cond: cond, hook: h})
}

// RegisterGlobalHook registers a hook that fires for every value.
func (p *Processor) RegisterGlobalHook(h Func) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.globalHooks = append(p.globalHooks, h)
}

// Process applies all applicable hooks to the value in the defined order:
// key hooks → value hooks → condition hooks → global hooks.
// Each hook's output becomes the next hook's input.
func (p *Processor) Process(key string, value any) any {
	// Snapshot hook slices under read lock to avoid holding lock during execution.
	p.mu.RLock()
	keyH := p.keyHooks[key]
	var valueH []Func
	if isComparable(value) {
		valueH = p.valueHooks[value]
	}
	condH := make([]conditionEntry, len(p.conditionHooks))
	copy(condH, p.conditionHooks)
	globalH := make([]Func, len(p.globalHooks))
	copy(globalH, p.globalHooks)
	p.mu.RUnlock()

	for _, h := range keyH {
		value = h(key, value)
	}
	for _, h := range valueH {
		value = h(key, value)
	}
	for _, entry := range condH {
		if entry.cond(key, value) {
			value = entry.hook(key, value)
		}
	}
	for _, h := range globalH {
		value = h(key, value)
	}

	return value
}

// isComparable checks if a value can be used as a map key.
func isComparable(v any) bool {
	defer func() { recover() }()
	_ = v == v
	return true
}
