// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/confiify/confii-go/v2/internal/dictutil"
)

// materializeEffectiveConfig snapshots the selected unresolved environment,
// runs the built-in startup materialization pipeline once, and publishes the resulting ready
// configuration. New calls it before the Config escapes. Lifecycle methods
// that already hold c.mu may call it while constructing a transactional
// candidate and restore both maps on failure.
func (c *Config[T]) materializeEffectiveConfig(ctx context.Context) error {
	if ctx == nil {
		return &ConfigError{Op: "Materialize", Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw := dictutil.DeepCopy(c.envConfig)
	resolved, err := c.applySecretHookRecursive(withSecretResolutionSession(ctx), "", raw)
	if err != nil {
		return err
	}
	c.unresolvedEnvConfig = raw
	c.envConfig = resolved
	c.validatedModel = nil
	c.revision++
	return nil
}

func (c *Config[T]) materializeEffectiveValue(ctx context.Context, keyPath string, value any) (any, error) {
	if ctx == nil {
		return nil, &ConfigError{Op: "Materialize", Key: keyPath, Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = withSecretResolutionSession(ctx)
	switch typed := dictutil.DeepCopyValue(value).(type) {
	case map[string]any:
		return c.applySecretHookRecursive(ctx, keyPath, typed)
	case []any:
		return c.applySecretHookToSlice(ctx, keyPath, typed)
	default:
		return c.materializeLeaf(ctx, keyPath, typed)
	}
}

func (c *Config[T]) materializeLeaf(ctx context.Context, keyPath string, value any) (any, error) {
	return c.hookProcessor.Process(ctx, keyPath, value)
}

func (c *Config[T]) applySecretHookRecursive(ctx context.Context, prefix string, source map[string]any) (map[string]any, error) {
	return c.applySecretHookRecursiveMode(ctx, prefix, source, true)
}

func (c *Config[T]) applySecretHookRecursiveMode(ctx context.Context, prefix string, source map[string]any, parallel bool) (map[string]any, error) {
	if source == nil {
		return nil, nil
	}
	if parallel && c.opts.SecretResolutionConcurrency > 1 && len(source) > 1 {
		return c.applySecretHookMapParallel(ctx, prefix, source)
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		keyPath := key
		if prefix != "" {
			keyPath = prefix + "." + key
		}
		var resolved any
		var err error
		switch typed := value.(type) {
		case map[string]any:
			resolved, err = c.applySecretHookRecursiveMode(ctx, keyPath, typed, false)
		case []any:
			resolved, err = c.applySecretHookToSlice(ctx, keyPath, typed)
		default:
			resolved, err = c.materializeLeaf(ctx, keyPath, value)
		}
		if err != nil {
			return nil, err
		}
		result[key] = dictutil.DeepCopyValue(resolved)
	}
	return result, nil
}

func (c *Config[T]) applySecretHookMapParallel(ctx context.Context, prefix string, source map[string]any) (map[string]any, error) {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]any, len(keys))
	workCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := min(c.opts.SecretResolutionConcurrency, len(keys))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if workCtx.Err() != nil {
					continue
				}
				key := keys[index]
				keyPath := key
				if prefix != "" {
					keyPath = prefix + "." + key
				}
				value := source[key]
				var resolved any
				var err error
				switch typed := value.(type) {
				case map[string]any:
					resolved, err = c.applySecretHookRecursiveMode(workCtx, keyPath, typed, false)
				case []any:
					resolved, err = c.applySecretHookToSlice(workCtx, keyPath, typed)
				default:
					resolved, err = c.materializeLeaf(workCtx, keyPath, value)
				}
				if err != nil {
					cancel(err)
					continue
				}
				values[index] = dictutil.DeepCopyValue(resolved)
			}
		}()
	}
sendJobs:
	for index := range keys {
		select {
		case jobs <- index:
		case <-workCtx.Done():
			break sendJobs
		}
		if workCtx.Err() != nil {
			break sendJobs
		}
	}
	close(jobs)
	wg.Wait()
	if cause := context.Cause(workCtx); cause != nil {
		return nil, cause
	}
	result := make(map[string]any, len(keys))
	for index, key := range keys {
		result[key] = values[index]
	}
	return result, nil
}

func (c *Config[T]) applySecretHookToSlice(ctx context.Context, keyPath string, source []any) ([]any, error) {
	result := make([]any, len(source))
	for index, value := range source {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var resolved any
		var err error
		switch typed := value.(type) {
		case map[string]any:
			resolved, err = c.applySecretHookRecursive(ctx, keyPath, typed)
		case []any:
			resolved, err = c.applySecretHookToSlice(ctx, keyPath, typed)
		default:
			resolved, err = c.materializeLeaf(ctx, keyPath, value)
		}
		if err != nil {
			return nil, err
		}
		result[index] = dictutil.DeepCopyValue(resolved)
	}
	return result, nil
}

// RefreshSecrets re-materializes the effective configuration from the
// retained unresolved selected environment. Ordinary Get, ToDict, Export,
// and Typed calls read the already-materialized snapshot and do not use this
// method. Refresh is explicit because it can perform remote provider I/O.
//
// The refresh is transactional: hooks and validation run against private
// snapshots without holding c.mu. The candidate is published only when the
// unresolved source state has not changed in the meantime and every check
// succeeds. On error the previous ready configuration remains live.
//
//	if err := cfg.RefreshSecrets(); err != nil {
//		return fmt.Errorf("refresh configuration secrets: %w", err)
//	}
//	app, err := cfg.Typed() // observes the newly published secret values
func (c *Config[T]) RefreshSecrets() error {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.RefreshSecretsWithContext(ctx)
}

// RefreshSecretsWithContext is the context-aware form of
// [Config.RefreshSecrets]. It clears the managed resolver cache when
// supported, resolves every retained reference, reruns enabled validation,
// and publishes only a complete candidate. A nil or canceled context, a
// frozen or closed Config, provider failure, validation failure, or concurrent
// source change returns an error and preserves the previous snapshot.
func (c *Config[T]) RefreshSecretsWithContext(ctx context.Context) error {
	if ctx == nil {
		return &ConfigError{Op: "RefreshSecrets", Err: fmt.Errorf("%w: nil context", ErrConfigInvalid)}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	started := time.Now()
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return NewClosedError("RefreshSecrets")
	}
	if c.frozen {
		c.mu.RUnlock()
		return NewFrozenError("RefreshSecrets")
	}
	raw := dictutil.DeepCopy(c.unresolvedEnvConfig)
	before := dictutil.DeepCopy(c.envConfig)
	c.mu.RUnlock()

	if raw == nil {
		return nil
	}
	if invalidator, ok := c.opts.SecretResolver.(interface{ ClearCache() }); ok {
		invalidator.ClearCache()
	}

	resolved, err := c.applySecretHookRecursive(withSecretResolutionSession(ctx), "", dictutil.DeepCopy(raw))
	if err != nil {
		return &ConfigError{
			Op:  "RefreshSecrets",
			Err: fmt.Errorf("%w: resolve effective configuration: %w", ErrConfigLoad, err),
		}
	}
	if err := c.validateMaterializedCandidate(resolved); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	if !reflect.DeepEqual(raw, c.unresolvedEnvConfig) {
		c.mu.Unlock()
		return &ConfigError{
			Op:  "RefreshSecrets",
			Err: fmt.Errorf("%w: configuration changed while secrets were being refreshed", ErrConfigLoad),
		}
	}
	c.envConfig = resolved
	c.validatedModel = nil
	c.revision++
	callbacks := c.snapshotChangeCallbacks()
	contextCallbacks := c.snapshotChangeContextCallbacks()
	oldFlat := dictutil.Flatten(before)
	newFlat := dictutil.Flatten(resolved)
	observer := c.observer
	emitter := c.eventEmitter
	c.mu.Unlock()

	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)
	c.notifyContextChangesUnlocked(ctx, contextCallbacks, oldFlat, newFlat)
	if observer != nil {
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.EmitWithContext(ctx, "secrets_refreshed", nil, time.Since(started))
		emitter.EmitWithContext(ctx, "change", before, dictutil.DeepCopy(resolved))
	}
	return nil
}
