// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"

	"github.com/confiify/confii-go/diff"
	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/observe"
)

// Diff compares this config with another config.
func (c *Config[T]) Diff(other *Config[T]) []diff.ConfigDiff {
	return diff.Diff(c.ToDict(), other.ToDict())
}

// DetectDrift compares this config against an intended baseline.
func (c *Config[T]) DetectDrift(intended map[string]any) []diff.ConfigDiff {
	return diff.Diff(intended, c.ToDict())
}

// EnableObservability enables access/reload/change metrics collection.
func (c *Config[T]) EnableObservability() *observe.Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observer == nil {
		c.observer = observe.NewMetrics(len(dictutil.FlatKeys(c.envConfig)))
	}
	return c.observer
}

// EnableEvents enables event emission.
func (c *Config[T]) EnableEvents() *observe.EventEmitter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eventEmitter == nil {
		c.eventEmitter = observe.NewEventEmitter(c.logger)
	}
	return c.eventEmitter
}

// EnableVersioning configures snapshot persistence under storagePath
// with a maxVersions ring cap. The configuration always takes effect:
// if a [observe.VersionManager] already exists — typically because
// [Config.SaveVersion] lazy-created one — it is reconfigured in place
// via [observe.VersionManager.Reconfigure], preserving the in-memory
// snapshot ring. Subsequent SaveVersion calls write to storagePath.
//
// EnableVersioning is safe to call multiple times; the latest call's
// arguments become authoritative.
func (c *Config[T]) EnableVersioning(storagePath string, maxVersions int) *observe.VersionManager {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager(storagePath, maxVersions)
	} else {
		c.versionMgr.Reconfigure(storagePath, maxVersions)
	}
	return c.versionMgr
}

// GetMetrics returns current observability metrics. Returns nil if not enabled.
func (c *Config[T]) GetMetrics() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.observer == nil {
		return nil
	}
	return c.observer.Statistics()
}

// SaveVersion captures the current config as an immutable version
// snapshot. If no [observe.VersionManager] exists yet, one is
// lazy-created with in-memory defaults; a later [Config.EnableVersioning]
// call reconfigures the same manager in place. The lazy allocation
// runs under c.mu so concurrent first callers cannot double-initialize.
func (c *Config[T]) SaveVersion(metadata map[string]any) (*observe.Version, error) {
	c.mu.Lock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager("", 0)
	}
	mgr := c.versionMgr
	envSnapshot := dictutil.DeepCopy(c.envConfig)
	c.mu.Unlock()
	return mgr.SaveVersion(envSnapshot, metadata)
}

// RollbackToVersion restores the config to a previous version snapshot.
func (c *Config[T]) RollbackToVersion(versionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.frozen {
		return NewFrozenError("RollbackToVersion")
	}
	if c.versionMgr == nil {
		return fmt.Errorf("versioning not enabled")
	}

	v := c.versionMgr.GetVersion(versionID)
	if v == nil {
		return fmt.Errorf("version %s not found", versionID)
	}

	// Deep-copy the snapshot before assigning into live state. Aliasing
	// v.Config directly let later mutations of mergedConfig corrupt the
	// stored snapshot, so a subsequent rollback to the same version would
	// observe drifted (post-mutation) state instead of the captured one.
	snapshot := dictutil.DeepCopy(v.Config)
	c.envConfig = snapshot
	c.mergedConfig = dictutil.DeepCopy(snapshot)
	c.validatedModel = nil
	return nil
}
