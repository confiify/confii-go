// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"testing"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func failSelectedSecret(_ context.Context, _ string, value any) (any, error) {
	if value == "${secret:fail}" {
		return nil, errors.New("secret unavailable")
	}
	return value, nil
}

func TestEagerSetAndOverrideRejectResolutionFailure(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"stable": "ready"}, WithSecretHook(failSelectedSecret))

	err := cfg.Set("runtime.secret", "${secret:fail}")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.False(t, cfg.Has("runtime.secret"))

	restore, err := cfg.Override(map[string]any{"runtime.secret": "${secret:fail}"})
	require.Error(t, err)
	assert.Nil(t, restore)
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.Equal(t, "ready", cfg.GetStringOr("stable", ""))
}

func TestEagerMutationsRecoverMissingRawSnapshot(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"stable": "ready"})
	cfg.mu.Lock()
	cfg.unresolvedEnvConfig = nil
	cfg.mu.Unlock()

	restore, err := cfg.Override(map[string]any{"temporary": true})
	require.NoError(t, err)
	assert.True(t, cfg.GetBoolOr("temporary", false))
	restore()
	assert.False(t, cfg.Has("temporary"))

	cfg.mu.Lock()
	cfg.unresolvedEnvConfig = nil
	cfg.mu.Unlock()
	require.NoError(t, cfg.ExtendWithContext(context.Background(), &refactorCoverageLoader{
		source: "extension", data: map[string]any{"extended": true},
	}))
	assert.True(t, cfg.GetBoolOr("extended", false))
}

func TestEagerExtendRollsBackResolutionFailure(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"stable": "ready"}, WithSecretHook(failSelectedSecret))
	err := cfg.ExtendWithContext(context.Background(), &refactorCoverageLoader{
		source: "extension", data: map[string]any{"new_secret": "${secret:fail}"},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
	assert.Equal(t, "ready", cfg.GetStringOr("stable", ""))
	assert.False(t, cfg.Has("new_secret"))
}

func TestEagerMutationRawPathFailuresRollback(t *testing.T) {
	t.Run("Set", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"parent": map[string]any{"original": true}})
		cfg.EnableObservability()
		cfg.EnableEvents()
		cfg.mu.Lock()
		cfg.unresolvedEnvConfig = map[string]any{"parent": "scalar"}
		cfg.mu.Unlock()

		err := cfg.Set("parent.child", "value")
		require.Error(t, err)
		assert.True(t, cfg.GetBoolOr("parent.original", false))
		assert.False(t, cfg.Has("parent.child"))
	})

	t.Run("Override", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"parent": map[string]any{"original": true}})
		cfg.mu.Lock()
		cfg.unresolvedEnvConfig = map[string]any{"parent": "scalar"}
		cfg.mu.Unlock()

		restore, err := cfg.Override(map[string]any{"parent.child": "value"})
		require.Error(t, err)
		assert.Nil(t, restore)
		assert.True(t, cfg.GetBoolOr("parent.original", false))
	})
}

func TestOverrideReplayHandlesRawPathConflict(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{
		"first":  false,
		"parent": map[string]any{"base": true},
	})
	restoreFirst, err := cfg.Override(map[string]any{"first": true})
	require.NoError(t, err)
	_, err = cfg.Override(map[string]any{"parent.child": "survivor"})
	require.NoError(t, err)

	cfg.mu.Lock()
	cfg.overrideBaseRawEnv = map[string]any{"first": false, "parent": "scalar"}
	cfg.mu.Unlock()
	restoreFirst()

	assert.Equal(t, "survivor", cfg.GetStringOr("parent.child", ""))
}

func TestSecretBackedInspectionFallbackBranches(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"plain": "value"})
	cfg.mu.Lock()
	cfg.unresolvedEnvConfig = nil
	assert.False(t, cfg.sensitivePathLocked("plain"))
	cfg.unresolvedEnvConfig = map[string]any{"other": "value"}
	assert.False(t, cfg.sensitivePathLocked("plain"))
	cfg.mu.Unlock()
}

func TestSecretBackedExplainRedactsHistory(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"password": "resolved"}, WithDebugMode(true))
	cfg.mu.Lock()
	cfg.unresolvedEnvConfig = map[string]any{"password": "${secret:database}"}
	cfg.sourceTracker.TrackValue("password", "older-secret", "runtime", "runtime", cfg.env)
	cfg.sourceTracker.TrackValue("password", "newer-secret", "runtime", "runtime", cfg.env)
	cfg.mu.Unlock()

	explanation := cfg.Explain("password")
	history, ok := explanation["override_history"].([]map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, history)
	for _, entry := range history {
		assert.Equal(t, redactedSecretValue, entry["value"])
	}
	assert.Equal(t, redactedSecretValue, explanation["current_value"])
}

func TestOverrideReplayFrameWithoutMaterializedPayloadUsesRawValue(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"key": "base"})
	restoreFirst, err := cfg.Override(map[string]any{"first": true})
	require.NoError(t, err)
	frame := &overrideFrame{id: 999, payload: map[string]any{"key": "fallback"}, applied: true}
	cfg.mu.Lock()
	cfg.overrideStack = append(cfg.overrideStack, frame)
	cfg.mu.Unlock()
	restoreFirst()
	assert.Equal(t, "fallback", cfg.GetStringOr("key", ""))
	assert.Equal(t, "fallback", dictutil.DeepCopy(cfg.unresolvedEnvConfig)["key"])
}
