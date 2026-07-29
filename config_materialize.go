// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/confiify/confii-go/hook"
	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/validate"
)

// materializeEffectiveConfig snapshots the selected unresolved environment,
// runs the built-in startup materialization pipeline once, and publishes the resulting ready
// configuration. New calls it before the Config escapes. Lifecycle methods
// that already hold c.mu may call it while constructing a transactional
// candidate and restore both maps on failure.
func (c *Config[T]) materializeEffectiveConfig(ctx context.Context) error {
	raw := dictutil.DeepCopy(c.envConfig)
	resolved, err := c.applySecretHookRecursive(withSecretResolutionSession(ctx), "", raw)
	if err != nil {
		return err
	}
	c.unresolvedEnvConfig = raw
	c.envConfig = resolved
	c.validatedModel = nil
	return nil
}

func (c *Config[T]) materializeEffectiveValue(ctx context.Context, keyPath string, value any) (any, error) {
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
	resolved := value
	// Match the built-in read-pipeline order without executing arbitrary
	// post-construction hooks: environment expansion, type casting, then the
	// constructor-time secret hook.
	if c.opts.UseEnvExpander {
		resolved = hook.NewEnvExpanderHook()(keyPath, resolved)
	}
	if c.opts.UseTypeCasting {
		resolved = hook.NewTypeCastHook()(keyPath, resolved)
	}
	if c.opts.SecretHook == nil {
		return resolved, nil
	}
	return c.opts.SecretHook(ctx, keyPath, resolved)
}

func (c *Config[T]) applySecretHookRecursive(ctx context.Context, prefix string, source map[string]any) (map[string]any, error) {
	if source == nil {
		return nil, nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		keyPath := key
		if prefix != "" {
			keyPath = prefix + "." + key
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
		result[key] = dictutil.DeepCopyValue(resolved)
	}
	return result, nil
}

func (c *Config[T]) applySecretHookToSlice(ctx context.Context, keyPath string, source []any) ([]any, error) {
	result := make([]any, len(source))
	for index, value := range source {
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
func (c *Config[T]) RefreshSecrets(ctx context.Context) error {
	started := time.Now()
	c.mu.RLock()
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

	c.mu.Lock()
	if !reflect.DeepEqual(raw, c.unresolvedEnvConfig) {
		c.mu.Unlock()
		return &ConfigError{
			Op:  "RefreshSecrets",
			Err: fmt.Errorf("%w: configuration changed while secrets were being refreshed", ErrConfigLoad),
		}
	}
	c.envConfig = resolved
	c.validatedModel = nil
	callbacks := c.snapshotChangeCallbacks()
	oldFlat := dictutil.Flatten(before)
	newFlat := dictutil.Flatten(resolved)
	observer := c.observer
	emitter := c.eventEmitter
	c.mu.Unlock()

	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)
	if observer != nil {
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("secrets_refreshed", nil, time.Since(started))
		emitter.Emit("change", before, dictutil.DeepCopy(resolved))
	}
	return nil
}

func (c *Config[T]) validateMaterializedCandidate(candidate map[string]any) error {
	if !c.opts.ValidateOnLoad {
		return nil
	}
	if c.jsonSchema != nil {
		messages, err := c.jsonSchema.ValidateDetailed(candidate)
		if err != nil {
			return &ConfigError{
				Op:  "RefreshSecrets",
				Err: fmt.Errorf("%w: schema validation failed for %d constraint(s)", ErrConfigValidation, max(1, len(messages))),
				Context: map[string]any{
					"schema_errors": messages,
				},
			}
		}
	}
	if configTypeSupportsStructValidation[T]() {
		if _, err := validate.DecodeAndValidate[T](candidate); err != nil {
			return NewValidationError([]string{err.Error()}, err)
		}
	}
	return nil
}
