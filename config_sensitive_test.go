// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resolvedSecretConfig(t *testing.T, resolved string) *Config[any] {
	t.Helper()
	cfg, err := New[any](
		WithLoaders(&contextMapLoader{name: "sensitive", data: map[string]any{
			"database": map[string]any{"password": "${secret@vault:database:password}"},
		}}),
		WithSecretHook(func(_ context.Context, _ string, value any) (any, error) {
			if value == "${secret@vault:database:password}" {
				return resolved, nil
			}
			return value, nil
		}),
		WithDebugMode(true),
	)
	require.NoError(t, err)
	return cfg
}

func TestSensitiveMetadataRedactsDiffDriftAndDetachedTracker(t *testing.T) {
	left := resolvedSecretConfig(t, "left-secret")
	right := resolvedSecretConfig(t, "right-secret")

	differences, err := left.Diff(right)
	require.NoError(t, err)
	require.Len(t, differences, 1)
	require.Len(t, differences[0].NestedDiffs, 1)
	assert.Equal(t, redactedSecretValue, differences[0].OldValue)
	assert.Equal(t, redactedSecretValue, differences[0].NewValue)
	assert.Equal(t, redactedSecretValue, differences[0].NestedDiffs[0].OldValue)
	assert.Equal(t, redactedSecretValue, differences[0].NestedDiffs[0].NewValue)

	drift, err := left.DetectDrift(map[string]any{"database": map[string]any{"password": "intended-secret"}})
	require.NoError(t, err)
	require.Len(t, drift, 1)
	assert.Equal(t, redactedSecretValue, drift[0].OldValue)
	assert.Equal(t, redactedSecretValue, drift[0].NewValue)

	tracker := left.SourceTracker()
	info := tracker.GetSourceInfo("database.password")
	require.NotNil(t, info)
	assert.Equal(t, redactedSecretValue, info.Value)
	tracker.TrackValue("database.password", "caller-mutation", "caller", "caller", "")
	assert.Equal(t, redactedSecretValue, left.Explain("database.password")["current_value"])
}

func TestSourceTrackerReturnsDetachedClone(t *testing.T) {
	cfg := resolvedSecretConfig(t, "detached-secret")

	tracker := cfg.SourceTracker()
	// Mutating a returned tracker must not reach Config's internal source
	// tracking: a second, independent call must not observe the injected key.
	tracker.TrackValue("caller.injected", "x", "caller", "caller", "")

	fresh := cfg.SourceTracker()
	assert.Nil(t, fresh.GetSourceInfo("caller.injected"),
		"SourceTracker must return a detached clone; caller mutation must not leak into Config")

	// Redaction applied to a returned view must not corrupt Config's retained
	// value: a fresh clone still resolves the real (redacted) sensitive leaf.
	info := fresh.GetSourceInfo("database.password")
	require.NotNil(t, info)
	assert.Equal(t, redactedSecretValue, info.Value)
}

func TestVersionRollbackRestoresSensitivityMetadata(t *testing.T) {
	cfg := resolvedSecretConfig(t, "versioned-secret")
	cfg.EnableVersioning("", 3)
	version, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	require.Equal(t, []string{"database.password"}, version.SensitivePaths)

	require.NoError(t, cfg.Set("database.password", "plain"))
	assert.Equal(t, "plain", cfg.Explain("database.password")["current_value"])
	plainVersion, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	differences, err := cfg.versionMgr.DiffVersions(version.VersionID, plainVersion.VersionID)
	require.NoError(t, err)
	require.Len(t, differences, 1)
	assert.Equal(t, redactedSecretValue, differences[0].OldValue)
	assert.Equal(t, redactedSecretValue, differences[0].NewValue)
	require.NoError(t, cfg.RollbackToVersion(version.VersionID))
	assert.Equal(t, redactedSecretValue, cfg.Explain("database.password")["current_value"])
}

func TestExplicitSensitivePathsProtectHookAndMutationValues(t *testing.T) {
	cfg, err := New[any](
		WithLoaders(&contextMapLoader{name: "custom", data: map[string]any{
			"credentials": map[string]any{"token": "hook-input"},
		}}),
		WithKeyHook("credentials.token", func(_ context.Context, _ string, _ any) (any, error) {
			return "hook-produced-secret", nil
		}),
		WithSensitivePaths("credentials"),
		WithDebugMode(true),
	)
	require.NoError(t, err)
	assert.Equal(t, redactedSecretValue, cfg.Explain("credentials.token")["current_value"])

	require.NoError(t, cfg.Set("credentials.token", "runtime-secret"))
	assert.Equal(t, redactedSecretValue, cfg.Explain("credentials.token")["current_value"])
	version, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"credentials"}, version.SensitivePaths)
}

func TestSensitivePathAdmissionOwnsAndValidatesInput(t *testing.T) {
	paths := []string{"z.token", "a.password", "z.token"}
	cfg, err := New[any](WithSensitivePaths(paths...))
	require.NoError(t, err)
	paths[0] = "caller.changed"
	assert.Equal(t, []string{"a.password", "z.token"}, cfg.opts.SensitivePaths)

	for _, invalid := range []string{"", " leading", "trailing ", ".leading", "trailing.", "a..b"} {
		_, err := New[any](WithSensitivePaths(invalid))
		require.ErrorIs(t, err, ErrConfigInvalid, invalid)
	}
}
