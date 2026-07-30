// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextProbeLoader struct {
	deadline time.Time
	has      bool
	wait     bool
}

func (l *contextProbeLoader) Load(ctx context.Context) (map[string]any, error) {
	l.deadline, l.has = ctx.Deadline()
	if l.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return map[string]any{"ready": true}, nil
}

func (l *contextProbeLoader) Name() string   { return "context-probe" }
func (l *contextProbeLoader) Source() string { return "context-probe" }

func TestNewAppliesImplicitStartupDeadline(t *testing.T) {
	loader := &contextProbeLoader{}
	started := time.Now()

	cfg, err := New[any](WithLoaders(loader))
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.True(t, loader.has)
	assert.WithinDuration(t, started.Add(60*time.Second), loader.deadline, time.Second)
}

func TestNewWithContextPreservesCallerDeadline(t *testing.T) {
	loader := &contextProbeLoader{}
	want := time.Now().Add(5 * time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()

	_, err := NewWithContext[any](ctx, WithLoaders(loader), WithStartupTimeout(time.Second))
	require.NoError(t, err)
	require.True(t, loader.has)
	assert.WithinDuration(t, want, loader.deadline, time.Second)
}

func TestStartupTimeoutCanBeDisabled(t *testing.T) {
	loader := &contextProbeLoader{}

	_, err := NewWithContext[any](context.Background(), WithLoaders(loader), WithStartupTimeout(0))
	require.NoError(t, err)
	assert.False(t, loader.has)
}

func TestStartupTimeoutCancelsInitialization(t *testing.T) {
	loader := &contextProbeLoader{wait: true}

	_, err := New[any](WithLoaders(loader), WithStartupTimeout(10*time.Millisecond))
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestCancellationCannotBeSuppressedByErrorPolicy(t *testing.T) {
	for _, policy := range []ErrorPolicy{ErrorPolicyWarn, ErrorPolicyIgnore} {
		t.Run(string(policy), func(t *testing.T) {
			_, err := New[any](WithLoaders(&contextProbeLoader{wait: true}),
				WithOnError(policy),
				WithStartupTimeout(10*time.Millisecond),
			)
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func TestSetAndOverrideWithContextCancelBeforeMutation(t *testing.T) {
	cfg, err := New[any](WithLoaders(&contextProbeLoader{}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, cfg.SetWithContext(ctx, "new.key", "value"), context.Canceled)
	_, err = cfg.OverrideWithContext(ctx, map[string]any{"new.key": "value"})
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, cfg.Has("new.key"))
}

func TestCloseIsIdempotentAndRejectsMutations(t *testing.T) {
	cfg, err := New[any](WithLoaders(&contextProbeLoader{}))
	require.NoError(t, err)
	require.NoError(t, cfg.Close())
	require.NoError(t, cfg.Close())
	require.ErrorIs(t, cfg.Set("key", "value"), ErrConfigClosed)
	require.ErrorIs(t, cfg.ReloadWithContext(context.Background()), ErrConfigClosed)
	_, err = cfg.Override(map[string]any{"key": "value"})
	require.ErrorIs(t, err, ErrConfigClosed)
}

func TestSelfConfigStartupTimeoutAndExplicitPrecedence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"),
		[]byte("startup:\n  timeout: 10ms\n"),
		0o600,
	))

	_, err := New[any](WithWorkingDir(dir),
		WithLoaders(&contextProbeLoader{wait: true}),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))

	probe := &contextProbeLoader{}
	_, err = New[any](WithWorkingDir(dir),
		WithLoaders(probe),
		WithStartupTimeout(0),
	)
	require.NoError(t, err)
	assert.False(t, probe.has, "explicit option must override .confii.yaml")
}

func TestNewWithContextRejectsInvalidContextAndTimeout(t *testing.T) {
	_, err := NewWithContext[any](nil) //nolint:staticcheck // verifies the public nil-context contract
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigLoad))

	_, err = New[any](WithStartupTimeout(-time.Second))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigLoad))

	_, err = New[any](WithOperationTimeout(-time.Second))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigLoad))
}

func TestImplicitRuntimeAPIsUseOperationTimeout(t *testing.T) {
	cfg, err := New[any](WithLoaders(&contextMapLoader{name: "runtime-timeout", data: map[string]any{}}),
		WithOperationTimeout(10*time.Millisecond),
		WithGlobalHook(func(ctx context.Context, _ string, value any) (any, error) {
			<-ctx.Done()
			return value, ctx.Err()
		}),
	)
	require.NoError(t, err)
	err = cfg.Set("key", "value")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestGetWithContextRejectsNilAndCanceledContextBeforeReading(t *testing.T) {
	cfg, err := New[any](WithLoaders(&contextMapLoader{name: "get-context", data: map[string]any{"key": "value"}}))
	require.NoError(t, err)
	_, err = cfg.GetWithContext(nil, "key") //nolint:staticcheck // verifies the public nil-context contract
	require.ErrorIs(t, err, ErrConfigInvalid)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = cfg.GetWithContext(ctx, "key")
	require.ErrorIs(t, err, context.Canceled)
}
