// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"

	"github.com/confiify/confii-go/v2/diff"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/observe"
)

// Diff compares this Config's materialized snapshot with other. Results use
// dot-separated leaf paths and classify each difference as added, removed, or
// modified; see [diff.ConfigDiff]. Passing nil returns an error. The method
// does not rerun hooks or contact secret providers.
func (c *Config[T]) Diff(other *Config[T]) ([]diff.ConfigDiff, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.DiffWithContext(ctx, other)
}

// DiffWithContext is the context-aware form of [Config.Diff]. The context is
// checked while snapshots are copied; a nil or canceled context returns an
// error and no partial comparison.
func (c *Config[T]) DiffWithContext(ctx context.Context, other *Config[T]) ([]diff.ConfigDiff, error) {
	if other == nil {
		return nil, fmt.Errorf("diff: other config is nil")
	}
	left, err := c.ToDictWithContext(ctx)
	if err != nil {
		return nil, err
	}
	right, err := other.ToDictWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return diff.Diff(left, right), nil
}

// DetectDrift compares intended with the current materialized snapshot. A key
// present only in the current snapshot is reported as added, a key present
// only in intended as removed, and unequal values as modified. intended is not
// retained or mutated.
func (c *Config[T]) DetectDrift(intended map[string]any) ([]diff.ConfigDiff, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.DetectDriftWithContext(ctx, intended)
}

// DetectDriftWithContext is the context-aware form of [Config.DetectDrift]. A
// nil or canceled context returns an error and no partial comparison.
func (c *Config[T]) DetectDriftWithContext(ctx context.Context, intended map[string]any) ([]diff.ConfigDiff, error) {
	current, err := c.ToDictWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return diff.Diff(intended, current), nil
}

// EnableObservability enables in-process metrics and returns the Config-owned,
// concurrency-safe collector. Repeated calls return the same collector and do
// not reset existing counters. Use [Config.GetMetrics] for a map snapshot.
func (c *Config[T]) EnableObservability() *observe.Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observer == nil {
		c.observer = observe.NewMetrics(len(dictutil.FlatKeys(c.envConfig)))
	}
	return c.observer
}

// EnableEvents enables synchronous lifecycle event delivery and returns the
// Config-owned, concurrency-safe emitter. Repeated calls return the same
// emitter. Register listeners on the returned value before the operations they
// must observe; events emitted before registration are not replayed.
func (c *Config[T]) EnableEvents() *observe.EventEmitter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eventEmitter == nil {
		c.eventEmitter = observe.NewEventEmitter(c.logger)
	}
	return c.eventEmitter
}

// EnableVersioning configures snapshot history and returns the Config-owned,
// concurrency-safe manager. storagePath selects optional disk persistence; an
// empty path keeps versions in memory only. maxVersions limits retained
// versions, with non-positive values selecting the package default. Repeated
// calls preserve retained in-memory versions and apply the latest settings to
// future saves.
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

// GetMetrics returns a detached snapshot of current counters, durations, and
// key counts, or nil when observability has not been enabled. Mutating the
// returned map does not affect the collector.
func (c *Config[T]) GetMetrics() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.observer == nil {
		return nil
	}
	return c.observer.Statistics()
}

// SaveVersion captures the current materialized configuration and metadata as
// an immutable version. If versioning has not been enabled, Confii creates an
// in-memory manager with default retention. Configure persistent storage first
// with [Config.EnableVersioning] when versions must survive process restart.
func (c *Config[T]) SaveVersion(metadata map[string]any) (*observe.Version, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.SaveVersionWithContext(ctx, metadata)
}

// SaveVersionWithContext is the context-aware form of [Config.SaveVersion]. A
// nil or canceled context returns an error. Local persistence is atomic but is
// not interrupted after a filesystem write begins; therefore a version may
// have been saved when cancellation is observed immediately after the write.
func (c *Config[T]) SaveVersionWithContext(ctx context.Context, metadata map[string]any) (*observe.Version, error) {
	if ctx == nil {
		return nil, fmt.Errorf("save version: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager("", 0)
	}
	mgr := c.versionMgr
	envSnapshot := dictutil.DeepCopy(c.envConfig)
	c.mu.Unlock()
	version, err := mgr.SaveVersion(envSnapshot, metadata)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return version, nil
}

// RollbackToVersion atomically replaces the current snapshot with versionID.
// The restored version contains materialized values, so RefreshSecrets is a
// no-op until a later source Reload reintroduces unresolved references.
func (c *Config[T]) RollbackToVersion(versionID string) error {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.RollbackToVersionWithContext(ctx, versionID)
}

// RollbackToVersionWithContext is the context-aware form of
// [Config.RollbackToVersion]. It returns an error for a nil or canceled
// context, a frozen or closed Config, disabled versioning, or an unknown ID.
// The operation performs no provider I/O and does not currently emit change
// callbacks.
func (c *Config[T]) RollbackToVersionWithContext(ctx context.Context, versionID string) error {
	if ctx == nil {
		return fmt.Errorf("rollback version: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.frozen {
		return NewFrozenError("RollbackToVersion")
	}
	if c.closed {
		return NewClosedError("RollbackToVersion")
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
	// Version snapshots intentionally contain the ready effective values. A
	// rollback therefore establishes a new reference-free source baseline;
	// later RefreshSecrets is a no-op until a loader reload restores refs.
	c.unresolvedEnvConfig = dictutil.DeepCopy(snapshot)
	c.mergedConfig = dictutil.DeepCopy(snapshot)
	c.validatedModel = nil
	c.revision++
	return nil
}
