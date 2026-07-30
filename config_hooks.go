// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/confiify/confii-go/v2/hook"
)

// Logger returns the [*slog.Logger] resolved at construction time.
//
// The returned pointer is the same value that internal subsystems
// (loaders, hooks, validators) write through, making it the canonical
// observable for [WithLogger] and the self-config `log_level` setting.
// Mutations to the logger reference itself (rebinding) are not
// supported through this accessor; supply a different logger via
// [WithLogger] at construction time instead.
func (c *Config[T]) Logger() *slog.Logger { return c.logger }

// applyHooksRecursive walks a configuration map and returns a deep copy
// with the hook pipeline (env expansion, secret resolution, type
// casting, and any registered custom hooks) applied to every leaf.
//
// Each leaf is processed via [hook.Plan.Process] with its absolute
// dot-separated key path. Slice elements are
// recursed into; their key path uses the parent key (no index suffix)
// to match the hook contract — every hook should be index-agnostic
// today, making a future indexed-key hook extension a deliberate change.
//
// The returned map is a fresh allocation and shares no mutable state with the
// input. This function is used while building candidate snapshots; reads do not
// rerun the plan.
//
// applyHooksRecursive is read-only with respect to Config state and holds
// no Config locks. Callers pass a private snapshot so hooks can safely call
// back into Config APIs without deadlocking. The hook processor manages its
// own concurrency.
func (c *Config[T]) applyHooksRecursive(ctx context.Context, prefix string, m map[string]any) (map[string]any, error) {
	return c.applyHookPlanToMap(ctx, c.hookProcessor.Snapshot(), prefix, m)
}

func (c *Config[T]) applyHookPlanToMap(ctx context.Context, plan hook.Plan, prefix string, m map[string]any) (map[string]any, error) {
	if ctx == nil {
		return nil, &ConfigError{Op: "Hooks", Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	if m == nil {
		return nil, nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		keyPath := k
		if prefix != "" {
			keyPath = prefix + "." + k
		}
		switch tv := v.(type) {
		case map[string]any:
			sub, err := c.applyHookPlanToMap(ctx, plan, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[k] = sub
		case []any:
			arr, err := c.applyHookPlanToSlice(ctx, plan, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[k] = arr
		default:
			processed, err := plan.Process(ctx, keyPath, v)
			if err != nil {
				return nil, err
			}
			out[k] = processed
		}
	}
	return out, nil
}

// applyHooksToSlice mirrors applyHooksRecursive for []any slices, which
// commonly appear in YAML/JSON configurations (e.g. lists of hosts,
// arrays of feature-flag overrides). Each element is hook-applied at
// the parent key path; nested maps and slices are recursed into.
func (c *Config[T]) applyHooksToSlice(ctx context.Context, keyPath string, items []any) ([]any, error) {
	return c.applyHookPlanToSlice(ctx, c.hookProcessor.Snapshot(), keyPath, items)
}

func (c *Config[T]) applyHookPlanToSlice(ctx context.Context, plan hook.Plan, keyPath string, items []any) ([]any, error) {
	if ctx == nil {
		return nil, &ConfigError{Op: "Hooks", Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	out := make([]any, len(items))
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch tv := item.(type) {
		case map[string]any:
			sub, err := c.applyHookPlanToMap(ctx, plan, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[i] = sub
		case []any:
			arr, err := c.applyHookPlanToSlice(ctx, plan, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[i] = arr
		default:
			processed, err := plan.Process(ctx, keyPath, item)
			if err != nil {
				return nil, err
			}
			out[i] = processed
		}
	}
	return out, nil
}
