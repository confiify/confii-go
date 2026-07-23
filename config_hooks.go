package confii

import (
	"context"
	"github.com/confiify/confii-go/hook"
	"log/slog"
)

// HookProcessor returns the hook processor for registering custom hooks.
func (c *Config[T]) HookProcessor() *hook.Processor { return c.hookProcessor }

// Logger returns the [*slog.Logger] resolved at construction time.
//
// The returned pointer is the same value that internal subsystems
// (loaders, hooks, validators) write through, making it the canonical
// observable for [WithLogger] and the self-config `log_level` setting
// (G04). Mutations to the logger reference itself (rebinding) are not
// supported through this accessor; supply a different logger via
// [WithLogger] at construction time instead.
func (c *Config[T]) Logger() *slog.Logger { return c.logger }

// applyHooksRecursive walks a configuration map and returns a deep copy
// with the hook pipeline (env expansion, secret resolution, type
// casting, and any registered custom hooks) applied to every leaf.
//
// Each leaf is processed via [hook.Processor.ProcessCtx] with its
// absolute dot-separated key path so per-key hooks see the same path
// they would for a scalar [Config.GetCtx] call. Slice elements are
// recursed into; their key path uses the parent key (no index suffix)
// to match the hook contract — every hook should be index-agnostic
// today, but this is documented here so a future indexed-key hook
// extension is a deliberate change.
//
// The returned map is a fresh allocation and shares no mutable state
// with the input, satisfying the hook-applied "defensive copy"
// contract that all access modes (Get whole-map, Typed, ToDict,
// Export) now rely on for G11.
//
// applyHooksRecursive is read-only with respect to Config state and holds
// no Config locks. Callers pass a private snapshot so hooks can safely call
// back into Config APIs without deadlocking. The hook processor manages its
// own concurrency.
func (c *Config[T]) applyHooksRecursive(ctx context.Context, prefix string, m map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		keyPath := k
		if prefix != "" {
			keyPath = prefix + "." + k
		}
		switch tv := v.(type) {
		case map[string]any:
			sub, err := c.applyHooksRecursive(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[k] = sub
		case []any:
			arr, err := c.applyHooksToSlice(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[k] = arr
		default:
			processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, v)
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
	out := make([]any, len(items))
	for i, item := range items {
		switch tv := item.(type) {
		case map[string]any:
			sub, err := c.applyHooksRecursive(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[i] = sub
		case []any:
			arr, err := c.applyHooksToSlice(ctx, keyPath, tv)
			if err != nil {
				return nil, err
			}
			out[i] = arr
		default:
			processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, item)
			if err != nil {
				return nil, err
			}
			out[i] = processed
		}
	}
	return out, nil
}
