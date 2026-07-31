// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollbackValidatesBeforePublication(t *testing.T) {
	rejectBlocked := validatorFunc(func(data map[string]any) error {
		if data["name"] == "blocked" {
			return errors.New("name is blocked")
		}
		return nil
	})
	cfg, err := confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "ready"}),
		confii.WithValidator(rejectBlocked),
	)
	require.NoError(t, err)
	versionDirectory := t.TempDir()
	manager := cfg.EnableVersioning(versionDirectory, 10)
	invalidID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	invalidRecord := `{"version_id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","config":{"name":"blocked"},"timestamp":"2026-01-01T00:00:00Z"}`
	require.NoError(t, os.WriteFile(filepath.Join(versionDirectory, invalidID+".json"), []byte(invalidRecord), 0o600))
	require.NotNil(t, manager.GetVersion(invalidID))

	err = cfg.RollbackToVersion(invalidID)
	require.ErrorIs(t, err, confii.ErrConfigValidation)
	assert.Equal(t, "ready", cfg.GetStringOr("name", ""))
}

func TestRollbackContextAndLifecycleAdmission(t *testing.T) {
	cfg, err := confii.New[any](confii.WithLoaders(extensionLoader{"name": "ready"}))
	require.NoError(t, err)
	var nilContext context.Context
	require.Error(t, cfg.RollbackToVersionWithContext(nilContext, "unused"))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, cfg.RollbackToVersionWithContext(canceled, "unused"), context.Canceled)
	require.Error(t, cfg.RollbackToVersion("unused"))

	version, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	require.NoError(t, cfg.Close())
	require.ErrorIs(t, cfg.RollbackToVersion(version.VersionID), confii.ErrConfigClosed)
}

func TestRollbackPublishesLifecycleAndVersionSource(t *testing.T) {
	cfg, err := confii.New[any](confii.WithLoaders(extensionLoader{"name": "baseline"}))
	require.NoError(t, err)
	version, err := cfg.SaveVersion(map[string]any{"purpose": "rollback target"})
	require.NoError(t, err)
	require.NoError(t, cfg.Set("name", "current"))

	var callbackCalls, contextCallbackCalls atomic.Int32
	cfg.OnChange(func(key string, oldValue, newValue any) {
		if key == "name" && oldValue == "current" && newValue == "baseline" {
			callbackCalls.Add(1)
		}
	})
	cfg.OnChangeWithContext(func(_ context.Context, key string, oldValue, newValue any) {
		if key == "name" && oldValue == "current" && newValue == "baseline" {
			contextCallbackCalls.Add(1)
		}
	})
	metrics := cfg.EnableObservability()
	events := cfg.EnableEvents()
	rollbackEvent := make(chan []any, 1)
	changeEvent := make(chan []any, 1)
	events.On("rollback", func(args ...any) { rollbackEvent <- args })
	events.On("change", func(args ...any) { changeEvent <- args })

	require.NoError(t, cfg.RollbackToVersionWithContext(context.Background(), version.VersionID))
	assert.Equal(t, "baseline", cfg.GetStringOr("name", ""))
	assert.EqualValues(t, 1, callbackCalls.Load())
	assert.EqualValues(t, 1, contextCallbackCalls.Load())
	assert.EqualValues(t, 1, metrics.Statistics()["change_count"])

	select {
	case args := <-rollbackEvent:
		require.Len(t, args, 2)
		assert.Equal(t, version.VersionID, args[0])
	case <-time.After(time.Second):
		t.Fatal("rollback event was not emitted")
	}
	select {
	case args := <-changeEvent:
		require.Len(t, args, 2)
	case <-time.After(time.Second):
		t.Fatal("change event was not emitted")
	}

	source := cfg.GetSourceInfo("name")
	require.NotNil(t, source)
	assert.Equal(t, "version:"+version.VersionID, source.SourceFile)
	assert.Equal(t, "version", source.LoaderType)
	assert.Equal(t, "baseline", source.Value)
}

func TestRollbackRejectsConcurrentFreeze(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var gate atomic.Bool
	validator := validatorFunc(func(data map[string]any) error {
		if gate.Load() && data["name"] == "baseline" {
			close(started)
			<-release
		}
		return nil
	})
	cfg, err := confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "baseline"}),
		confii.WithValidator(validator),
	)
	require.NoError(t, err)
	version, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	require.NoError(t, cfg.Set("name", "current"))

	gate.Store(true)
	done := make(chan error, 1)
	go func() {
		done <- cfg.RollbackToVersionWithContext(context.Background(), version.VersionID)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("rollback validation did not start")
	}
	cfg.Freeze()
	close(release)
	require.ErrorIs(t, <-done, confii.ErrConfigFrozen)
	assert.Equal(t, "current", cfg.GetStringOr("name", ""))
}

func TestRollbackRejectsConcurrentClose(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var gate atomic.Bool
	validator := validatorFunc(func(data map[string]any) error {
		if gate.Load() && data["name"] == "baseline" {
			close(started)
			<-release
		}
		return nil
	})
	cfg, err := confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "baseline"}),
		confii.WithValidator(validator),
	)
	require.NoError(t, err)
	version, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	require.NoError(t, cfg.Set("name", "current"))

	gate.Store(true)
	done := make(chan error, 1)
	go func() {
		done <- cfg.RollbackToVersionWithContext(context.Background(), version.VersionID)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("rollback validation did not start")
	}
	require.NoError(t, cfg.Close())
	close(release)
	require.ErrorIs(t, <-done, confii.ErrConfigClosed)
	assert.Equal(t, "current", cfg.GetStringOr("name", ""))
}

func TestRollbackRejectsCancellationDuringValidation(t *testing.T) {
	var cancel context.CancelFunc
	var gate atomic.Bool
	validator := validatorFunc(func(data map[string]any) error {
		if gate.Load() && data["name"] == "baseline" {
			cancel()
		}
		return nil
	})
	cfg, err := confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "baseline"}),
		confii.WithValidator(validator),
	)
	require.NoError(t, err)
	version, err := cfg.SaveVersion(nil)
	require.NoError(t, err)
	require.NoError(t, cfg.Set("name", "current"))

	ctx, cancelContext := context.WithCancel(context.Background())
	cancel = cancelContext
	gate.Store(true)
	require.ErrorIs(t, cfg.RollbackToVersionWithContext(ctx, version.VersionID), context.Canceled)
	assert.Equal(t, "current", cfg.GetStringOr("name", ""))
}
