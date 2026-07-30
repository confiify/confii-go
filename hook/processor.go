// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package hook provides a thread-safe hook processor for transforming
// configuration values while a configuration snapshot is materialized.
package hook

import (
	"context"
	"errors"
	"reflect"
	"sync"
)

// Func transforms a configuration value during snapshot materialization.
//
// Every hook receives the caller's context, the full dot-separated key path,
// and the value produced by the previous hook. A hook may return an error to
// stop the pipeline. This is the sole hook signature in v2: transformations
// cannot silently discard cancellation, provider, or validation failures.
type Func func(ctx context.Context, key string, value any) (any, error)

// Condition determines whether a conditional hook should fire.
//
// Conditions share the hook operation's context and may return an error. An
// error stops the pipeline before the associated hook executes.
type Condition func(ctx context.Context, key string, value any) (bool, error)

// Processor manages hook registration and execution. It is safe for
// concurrent use.
//
// Each registration advances the processor revision. Callers that process a
// compound value should capture one [Plan] with [Processor.Snapshot()] and use
// it for the entire operation; this prevents concurrent registration from
// producing a result assembled under multiple hook sets.
type Processor struct {
	mu             sync.RWMutex
	revision       uint64
	keyHooks       map[string][]Func
	valueHooks     []valueEntry
	conditionHooks []conditionEntry
	globalHooks    []Func
}

type valueEntry struct {
	value any
	hooks []Func
}

type conditionEntry struct {
	cond Condition
	hook Func
}

// Plan is an immutable snapshot of a processor's hook pipeline.
//
// A Plan may be reused concurrently. Its Revision identifies the registration
// state from which it was captured.
type Plan struct {
	revision       uint64
	keyHooks       map[string][]Func
	valueHooks     []valueEntry
	conditionHooks []conditionEntry
	globalHooks    []Func
}

// NewProcessor creates an empty hook processor.
func NewProcessor() *Processor {
	return &Processor{keyHooks: make(map[string][]Func)}
}

// RegisterKeyHook registers a hook that fires when the key exactly matches.
func (p *Processor) RegisterKeyHook(key string, h Func) {
	if h == nil {
		panic("hook: RegisterKeyHook called with nil hook")
	}
	p.mu.Lock()
	p.keyHooks[key] = append(p.keyHooks[key], h)
	p.revision++
	p.mu.Unlock()
}

// RegisterValueHook registers a hook selected when the value entering the
// value-hook stage deeply equals value. Key hooks execute first, so their
// output determines which value hooks match. Non-comparable map and slice
// values are supported.
func (p *Processor) RegisterValueHook(value any, h Func) {
	if h == nil {
		panic("hook: RegisterValueHook called with nil hook")
	}
	p.mu.Lock()
	for i := range p.valueHooks {
		if reflect.DeepEqual(p.valueHooks[i].value, value) {
			p.valueHooks[i].hooks = append(p.valueHooks[i].hooks, h)
			p.revision++
			p.mu.Unlock()
			return
		}
	}
	p.valueHooks = append(p.valueHooks, valueEntry{value: value, hooks: []Func{h}})
	p.revision++
	p.mu.Unlock()
}

// RegisterConditionHook registers a hook that fires when cond returns true.
func (p *Processor) RegisterConditionHook(cond Condition, h Func) {
	if cond == nil {
		panic("hook: RegisterConditionHook called with nil condition")
	}
	if h == nil {
		panic("hook: RegisterConditionHook called with nil hook")
	}
	p.mu.Lock()
	p.conditionHooks = append(p.conditionHooks, conditionEntry{cond: cond, hook: h})
	p.revision++
	p.mu.Unlock()
}

// RegisterGlobalHook registers a hook that fires for every value.
func (p *Processor) RegisterGlobalHook(h Func) {
	if h == nil {
		panic("hook: RegisterGlobalHook called with nil hook")
	}
	p.mu.Lock()
	p.globalHooks = append(p.globalHooks, h)
	p.revision++
	p.mu.Unlock()
}

// Revision returns the current hook-registration revision.
func (p *Processor) Revision() uint64 {
	p.mu.RLock()
	revision := p.revision
	p.mu.RUnlock()
	return revision
}

// Snapshot returns an immutable plan for one logical processing operation.
func (p *Processor) Snapshot() Plan {
	p.mu.RLock()
	defer p.mu.RUnlock()

	plan := Plan{
		revision:       p.revision,
		keyHooks:       make(map[string][]Func, len(p.keyHooks)),
		valueHooks:     make([]valueEntry, len(p.valueHooks)),
		conditionHooks: append([]conditionEntry(nil), p.conditionHooks...),
		globalHooks:    append([]Func(nil), p.globalHooks...),
	}
	for key, hooks := range p.keyHooks {
		plan.keyHooks[key] = append([]Func(nil), hooks...)
	}
	for i, entry := range p.valueHooks {
		plan.valueHooks[i] = valueEntry{
			value: entry.value,
			hooks: append([]Func(nil), entry.hooks...),
		}
	}
	return plan
}

// Revision returns the processor revision captured by the plan.
func (p Plan) Revision() uint64 { return p.revision }

// Process captures the current hook plan and applies it to value.
func (p *Processor) Process(ctx context.Context, key string, value any) (any, error) {
	return p.Snapshot().Process(ctx, key, value)
}

// Process applies the plan's hooks in this order:
// key hooks → value hooks → condition hooks → global hooks.
// Each hook's output becomes the next hook's input.
func (p Plan) Process(ctx context.Context, key string, value any) (any, error) {
	if ctx == nil {
		return value, errors.New("hook: nil context")
	}
	if err := ctx.Err(); err != nil {
		return value, err
	}
	var err error
	for _, h := range p.keyHooks[key] {
		if err := ctx.Err(); err != nil {
			return value, err
		}
		value, err = h(ctx, key, value)
		if err != nil {
			return value, err
		}
	}
	valueHooks := p.matchingValueHooks(value)
	for _, h := range valueHooks {
		if err := ctx.Err(); err != nil {
			return value, err
		}
		value, err = h(ctx, key, value)
		if err != nil {
			return value, err
		}
	}
	for _, entry := range p.conditionHooks {
		if err := ctx.Err(); err != nil {
			return value, err
		}
		matched, conditionErr := entry.cond(ctx, key, value)
		if conditionErr != nil {
			return value, conditionErr
		}
		if !matched {
			continue
		}
		value, err = entry.hook(ctx, key, value)
		if err != nil {
			return value, err
		}
	}
	for _, h := range p.globalHooks {
		if err := ctx.Err(); err != nil {
			return value, err
		}
		value, err = h(ctx, key, value)
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func (p Plan) matchingValueHooks(value any) []Func {
	for _, entry := range p.valueHooks {
		if reflect.DeepEqual(entry.value, value) {
			return entry.hooks
		}
	}
	return nil
}
