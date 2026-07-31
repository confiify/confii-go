// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/confiify/confii-go/v2/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type runtimeMutationValidatorFunc func(map[string]any) error

func (fn runtimeMutationValidatorFunc) Validate(data map[string]any) error { return fn(data) }

func TestRejectedRuntimeMutationsDoNotPublishPartialState(t *testing.T) {
	rejectBlocked := runtimeMutationValidatorFunc(func(data map[string]any) error {
		if blocked, _ := data["blocked"].(bool); blocked {
			return errors.New("blocked value rejected")
		}
		return nil
	})

	t.Run("Set", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		cfg.opts.ValidateOnLoad = true
		cfg.opts.Validators = []Validator{rejectBlocked}
		beforeRevision := configRevision(cfg)
		before, err := cfg.ToDict()
		require.NoError(t, err)
		beforeTracker := cfg.sourceTracker.Snapshot()

		err = cfg.Set("blocked", true)
		require.ErrorIs(t, err, ErrConfigValidation)
		after, snapshotErr := cfg.ToDict()
		require.NoError(t, snapshotErr)
		assert.Equal(t, before, after)
		assert.Equal(t, beforeRevision, configRevision(cfg))
		assert.Equal(t, beforeTracker, cfg.sourceTracker.Snapshot())
		assert.Nil(t, cfg.GetSourceInfo("blocked"))
	})

	t.Run("Override", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		cfg.opts.ValidateOnLoad = true
		cfg.opts.Validators = []Validator{rejectBlocked}
		cfg.Freeze()
		beforeRevision := configRevision(cfg)
		before, err := cfg.ToDict()
		require.NoError(t, err)
		beforeTracker := cfg.sourceTracker.Snapshot()

		restore, err := cfg.Override(map[string]any{"blocked": true})
		require.ErrorIs(t, err, ErrConfigValidation)
		assert.Nil(t, restore)
		after, snapshotErr := cfg.ToDict()
		require.NoError(t, snapshotErr)
		assert.Equal(t, before, after)
		assert.Equal(t, beforeRevision, configRevision(cfg))
		assert.Equal(t, beforeTracker, cfg.sourceTracker.Snapshot())
		assert.True(t, cfg.IsFrozen())
		assert.Empty(t, cfg.overrideStack)
		assert.Nil(t, cfg.overrideBaseEnv)
		assert.Nil(t, cfg.overrideBaseRawEnv)
		assert.Nil(t, cfg.overrideBaseMerged)
		assert.Zero(t, cfg.overrideIDCounter)
	})
}

func TestRuntimeMutationValidatorMayReadConfig(t *testing.T) {
	for _, operation := range []string{"Set", "Override"} {
		t.Run(operation, func(t *testing.T) {
			cfg := newTestConfig(t, map[string]any{"stable": "published"})
			cfg.opts.ValidateOnLoad = true
			cfg.opts.Validators = []Validator{runtimeMutationValidatorFunc(func(_ map[string]any) error {
				value, err := cfg.Get("stable")
				if err != nil {
					return err
				}
				if value != "published" {
					return errors.New("validator observed unexpected live value")
				}
				return nil
			})}

			done := make(chan error, 1)
			go func() {
				if operation == "Set" {
					done <- cfg.Set("candidate", true)
					return
				}
				restore, err := cfg.Override(map[string]any{"candidate": true})
				if err == nil {
					restore()
				}
				done <- err
			}()

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("runtime mutation deadlocked while validator read Config")
			}
		})
	}
}

func TestRuntimeMutationRetriesAfterValidatorPublishesAnotherRevision(t *testing.T) {
	for _, operation := range []string{"Set", "Override"} {
		t.Run(operation, func(t *testing.T) {
			cfg := newTestConfig(t, map[string]any{"stable": true})
			var published atomic.Bool
			cfg.opts.ValidateOnLoad = true
			cfg.opts.Validators = []Validator{runtimeMutationValidatorFunc(func(_ map[string]any) error {
				if published.CompareAndSwap(false, true) {
					return cfg.Set("validator.published", true)
				}
				return nil
			})}

			if operation == "Set" {
				require.NoError(t, cfg.Set("requested", true))
			} else {
				restore, err := cfg.Override(map[string]any{"requested": true})
				require.NoError(t, err)
				defer restore()
			}

			assert.True(t, cfg.GetBoolOr("validator.published", false))
			assert.True(t, cfg.GetBoolOr("requested", false))
		})
	}
}

func TestRuntimeMutationRetriesStaleValidationFailure(t *testing.T) {
	for _, operation := range []string{"Set", "Override"} {
		t.Run(operation, func(t *testing.T) {
			cfg := newTestConfig(t, map[string]any{"stable": true})
			var first atomic.Bool
			cfg.opts.ValidateOnLoad = true
			cfg.opts.Validators = []Validator{runtimeMutationValidatorFunc(func(_ map[string]any) error {
				if first.CompareAndSwap(false, true) {
					require.NoError(t, cfg.Set("validator.published", true))
					return errors.New("failure from stale candidate")
				}
				return nil
			})}

			if operation == "Set" {
				require.NoError(t, cfg.Set("requested", true))
			} else {
				restore, err := cfg.Override(map[string]any{"requested": true})
				require.NoError(t, err)
				defer restore()
			}
			assert.True(t, cfg.GetBoolOr("validator.published", false))
			assert.True(t, cfg.GetBoolOr("requested", false))
		})
	}
}

func TestRuntimeMutationCancellationSupersedesValidationFailure(t *testing.T) {
	for _, operation := range []string{"Set", "Override"} {
		t.Run(operation, func(t *testing.T) {
			cfg := newTestConfig(t, map[string]any{"stable": true})
			ctx, cancel := context.WithCancel(context.Background())
			cfg.opts.ValidateOnLoad = true
			cfg.opts.Validators = []Validator{runtimeMutationValidatorFunc(func(_ map[string]any) error {
				cancel()
				return errors.New("candidate rejected")
			})}

			var err error
			if operation == "Set" {
				err = cfg.SetWithContext(ctx, "requested", true)
			} else {
				_, err = cfg.OverrideWithContext(ctx, map[string]any{"requested": true})
			}
			require.ErrorIs(t, err, context.Canceled)
			assert.False(t, cfg.Has("requested"))
		})
	}
}

func TestRuntimeMutationConflictHonorsContext(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"stable": true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conflict, err := cfg.runtimeMutationConflict(ctx, "Set", configRevision(cfg))
	assert.False(t, conflict)
	require.ErrorIs(t, err, context.Canceled)

	require.NoError(t, cfg.Close())
	conflict, err = cfg.runtimeMutationConflict(context.Background(), "Set", configRevision(cfg))
	assert.False(t, conflict)
	require.ErrorIs(t, err, ErrConfigClosed)
}

func TestRuntimeMutationsRecheckLifecycleAfterMaterialization(t *testing.T) {
	t.Run("nil contexts", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		var nilContext context.Context
		require.ErrorIs(t, cfg.SetWithContext(nilContext, "requested", true), ErrConfigInvalid)
		_, err := cfg.OverrideWithContext(nilContext, map[string]any{"requested": true})
		require.ErrorIs(t, err, ErrConfigInvalid)
	})

	t.Run("Set canceled by hook", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		ctx, cancel := context.WithCancel(context.Background())
		cfg.hookProcessor.RegisterKeyHook("requested", hook.Func(func(_ context.Context, _ string, value any) (any, error) {
			cancel()
			return value, nil
		}))
		require.ErrorIs(t, cfg.SetWithContext(ctx, "requested", true), context.Canceled)
		assert.False(t, cfg.Has("requested"))
	})

	t.Run("Set closed by hook", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		cfg.hookProcessor.RegisterKeyHook("requested", hook.Func(func(_ context.Context, _ string, value any) (any, error) {
			return value, cfg.Close()
		}))
		require.ErrorIs(t, cfg.Set("requested", true), ErrConfigClosed)
		assert.False(t, cfg.Has("requested"))
	})

	t.Run("Set frozen by hook", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		cfg.hookProcessor.RegisterKeyHook("requested", hook.Func(func(_ context.Context, _ string, value any) (any, error) {
			cfg.Freeze()
			return value, nil
		}))
		require.ErrorIs(t, cfg.Set("requested", true), ErrConfigFrozen)
		assert.False(t, cfg.Has("requested"))
	})

	t.Run("Override canceled between values", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		ctx, cancel := context.WithCancel(context.Background())
		cfg.hookProcessor.RegisterKeyHook("a", hook.Func(func(_ context.Context, _ string, value any) (any, error) {
			cancel()
			return value, nil
		}))
		_, err := cfg.OverrideWithContext(ctx, map[string]any{"a": true, "b": true})
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, cfg.Has("a"))
		assert.False(t, cfg.Has("b"))
	})

	t.Run("Override closed by hook", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"stable": true})
		cfg.hookProcessor.RegisterKeyHook("requested", hook.Func(func(_ context.Context, _ string, value any) (any, error) {
			return value, cfg.Close()
		}))
		_, err := cfg.Override(map[string]any{"requested": true})
		require.ErrorIs(t, err, ErrConfigClosed)
		assert.False(t, cfg.Has("requested"))
	})
}

func TestRuntimeMutationsRecheckLifecycleBeforePublication(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		action    func(context.CancelFunc, *Config[any])
		want      error
	}{
		{name: "Set canceled", operation: "Set", action: func(cancel context.CancelFunc, _ *Config[any]) { cancel() }, want: context.Canceled},
		{name: "Set closed", operation: "Set", action: func(_ context.CancelFunc, cfg *Config[any]) { _ = cfg.Close() }, want: ErrConfigClosed},
		{name: "Set frozen", operation: "Set", action: func(_ context.CancelFunc, cfg *Config[any]) { cfg.Freeze() }, want: ErrConfigFrozen},
		{name: "Override canceled", operation: "Override", action: func(cancel context.CancelFunc, _ *Config[any]) { cancel() }, want: context.Canceled},
		{name: "Override closed", operation: "Override", action: func(_ context.CancelFunc, cfg *Config[any]) { _ = cfg.Close() }, want: ErrConfigClosed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newTestConfig(t, map[string]any{"stable": true})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg.opts.ValidateOnLoad = true
			cfg.opts.Validators = []Validator{runtimeMutationValidatorFunc(func(_ map[string]any) error {
				test.action(cancel, cfg)
				return nil
			})}

			var err error
			if test.operation == "Set" {
				err = cfg.SetWithContext(ctx, "requested", true)
			} else {
				_, err = cfg.OverrideWithContext(ctx, map[string]any{"requested": true})
			}
			require.ErrorIs(t, err, test.want)
			assert.False(t, cfg.Has("requested"))
		})
	}
}
