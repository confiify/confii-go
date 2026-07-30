// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"testing"

	"github.com/confiify/confii-go/v2/hook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type materializedValidatedModel struct {
	Name string `confii:"name" validate:"required"`
}

func TestMaterializationInternalBranches(t *testing.T) {
	t.Parallel()
	cfg := &Config[any]{opts: defaultOptions(), hookProcessor: hook.NewProcessor()}
	resolved, err := cfg.applySecretHookRecursive(context.Background(), "", nil)
	require.NoError(t, err)
	assert.Nil(t, resolved)

	cfg.opts.SecretHook = func(_ context.Context, _ string, value any) (any, error) {
		if value == "fail" {
			return nil, errors.New("resolution failed")
		}
		return value, nil
	}
	cfg.hookProcessor.RegisterGlobalHook(cfg.opts.SecretHook)
	_, err = cfg.applySecretHookToSlice(context.Background(), "items", []any{
		[]any{"fail"},
	})
	require.EqualError(t, err, "resolution failed")
}

func TestMaterializedStructValidation(t *testing.T) {
	t.Parallel()
	cfg := &Config[materializedValidatedModel]{opts: defaultOptions()}
	cfg.opts.ValidateOnLoad = true
	require.Error(t, cfg.validateMaterializedCandidate(map[string]any{}))
	require.NoError(t, cfg.validateMaterializedCandidate(map[string]any{"name": "ready"}))
}

func TestSecretResolutionSessionContextBranches(t *testing.T) {
	t.Parallel()
	ctx := withSecretResolutionSession(nil) //nolint:staticcheck // verifies defensive handling of an internal nil context
	require.NotNil(t, ctx)

	calls := 0
	value, err := getSelfConfigSecretOnce(context.Background(), func(context.Context, string, string, string, string) (any, error) {
		calls++
		return "direct", nil
	}, "vault", "key", "", "")
	require.NoError(t, err)
	assert.Equal(t, "direct", value)
	assert.Equal(t, 1, calls)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = getSelfConfigSecretOnce(ctx, func(context.Context, string, string, string, string) (any, error) {
			close(started)
			<-release
			return "eventual", nil
		}, "vault", "shared", "", "")
	}()
	<-started
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = getSelfConfigSecretOnce(canceled, func(context.Context, string, string, string, string) (any, error) {
		t.Fatal("deduplicated waiter must not call the provider")
		return nil, nil
	}, "vault", "shared", "", "")
	assert.ErrorIs(t, err, context.Canceled)
	close(release)
	<-done
}
