// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"

	"github.com/confiify/confii-go/v2/diff"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/observe"
	"github.com/confiify/confii-go/v2/sourcetrack"
)

// MetricsReader exposes detached observability snapshots without granting
// access to Confii's mutable metrics collector.
type MetricsReader interface {
	Statistics() map[string]any
}

// EventSubscriber registers and removes lifecycle listeners without exposing
// the Config-owned event emitter's Emit operations. Listener delivery is
// synchronous and follows registration order.
type EventSubscriber interface {
	On(event string, listener func(args ...any)) EventSubscriber
	OnWithContext(event string, listener func(context.Context, ...any)) EventSubscriber
	Off(event string)
	OffWithContext(event string)
}

// VersionReader provides detached access to Config-owned version history.
// Saving, rollback, retention, and storage reconfiguration remain lifecycle
// operations on Config.
type VersionReader interface {
	GetVersion(versionID string) *observe.Version
	ListVersions() []*observe.Version
	LatestVersion() *observe.Version
	DiffVersions(firstVersionID, secondVersionID string) ([]diff.ConfigDiff, error)
}

type metricsReader struct{ metrics *observe.Metrics }

func (reader *metricsReader) Statistics() map[string]any {
	return reader.metrics.Statistics()
}

type eventSubscriber struct{ emitter *observe.EventEmitter }

func (subscriber *eventSubscriber) On(event string, listener func(args ...any)) EventSubscriber {
	subscriber.emitter.On(event, listener)
	return subscriber
}

func (subscriber *eventSubscriber) OnWithContext(event string, listener func(context.Context, ...any)) EventSubscriber {
	subscriber.emitter.OnWithContext(event, listener)
	return subscriber
}

func (subscriber *eventSubscriber) Off(event string) {
	subscriber.emitter.Off(event)
}

func (subscriber *eventSubscriber) OffWithContext(event string) {
	subscriber.emitter.OffWithContext(event)
}

type versionReader struct{ manager *observe.VersionManager }

func (reader *versionReader) GetVersion(versionID string) *observe.Version {
	return reader.manager.GetVersion(versionID)
}

func (reader *versionReader) ListVersions() []*observe.Version {
	return reader.manager.ListVersions()
}

func (reader *versionReader) LatestVersion() *observe.Version {
	return reader.manager.LatestVersion()
}

func (reader *versionReader) DiffVersions(firstVersionID, secondVersionID string) ([]diff.ConfigDiff, error) {
	return reader.manager.DiffVersions(firstVersionID, secondVersionID)
}

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
// checked while snapshots are copied. A nil target returns a structured
// [ErrConfigInvalid] error; a nil or canceled context returns an error and no
// partial comparison.
func (c *Config[T]) DiffWithContext(ctx context.Context, other *Config[T]) ([]diff.ConfigDiff, error) {
	if other == nil {
		return nil, NewInvalidError("Diff", "", errors.New("other config is nil"))
	}
	left, err := c.ToDictWithContext(ctx)
	if err != nil {
		return nil, err
	}
	right, err := other.ToDictWithContext(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	paths := cloneSensitivePaths(c.sensitivePaths)
	c.mu.RUnlock()
	other.mu.RLock()
	for path := range other.sensitivePaths {
		paths[path] = struct{}{}
	}
	other.mu.RUnlock()
	return redactConfigDiffs(diff.Diff(left, right), paths), nil
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
	c.mu.RLock()
	paths := cloneSensitivePaths(c.sensitivePaths)
	c.mu.RUnlock()
	return redactConfigDiffs(diff.Diff(intended, current), paths), nil
}

// EnableObservability enables in-process metrics and returns a read-only view.
// Repeated calls return the same view and do not reset existing counters. Use
// [Config.DisableObservability] to pause collection and [Config.ResetMetrics]
// to clear retained values.
func (c *Config[T]) EnableObservability() MetricsReader {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observer == nil {
		c.observer = observe.NewMetrics(len(dictutil.FlatKeys(c.envConfig)))
	}
	c.observer.Enable()
	if c.metricsReader == nil {
		c.metricsReader = &metricsReader{metrics: c.observer}
	}
	return c.metricsReader
}

// DisableObservability pauses metrics collection without discarding retained
// counters. It is a no-op when observability has not been enabled.
func (c *Config[T]) DisableObservability() {
	c.mu.RLock()
	observer := c.observer
	c.mu.RUnlock()
	if observer != nil {
		observer.Disable()
	}
}

// ResetMetrics clears retained counters and timings without disabling future
// collection. It is a no-op when observability has not been enabled.
func (c *Config[T]) ResetMetrics() {
	c.mu.RLock()
	observer := c.observer
	c.mu.RUnlock()
	if observer != nil {
		observer.Reset()
	}
}

// EnableEvents enables synchronous lifecycle event delivery and returns a
// subscription-only view. Callers can register and remove listeners but cannot
// fabricate Config lifecycle events. Repeated calls return the same view.
func (c *Config[T]) EnableEvents() EventSubscriber {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.eventEmitter == nil {
		c.eventEmitter = observe.NewEventEmitter(c.logger)
	}
	if c.eventSubscriber == nil {
		c.eventSubscriber = &eventSubscriber{emitter: c.eventEmitter}
	}
	return c.eventSubscriber
}

// EnableVersioning configures snapshot history and returns a read-only view.
// storagePath selects optional disk persistence; an
// empty path keeps versions in memory only. maxVersions limits retained
// versions, with non-positive values selecting the package default. Repeated
// calls preserve retained in-memory versions and apply the latest settings to
// future saves.
func (c *Config[T]) EnableVersioning(storagePath string, maxVersions int) VersionReader {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager(storagePath, maxVersions)
	} else {
		c.versionMgr.Reconfigure(storagePath, maxVersions)
	}
	if c.versionReader == nil {
		c.versionReader = &versionReader{manager: c.versionMgr}
	}
	return c.versionReader
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
		return nil, NewInvalidError("SaveVersion", "", errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.versionMgr == nil {
		c.versionMgr = observe.NewVersionManager("", 0)
		c.versionReader = &versionReader{manager: c.versionMgr}
	}
	mgr := c.versionMgr
	envSnapshot := dictutil.DeepCopy(c.envConfig)
	sensitivePaths := sensitivePathList(c.sensitivePaths)
	c.mu.Unlock()
	version, err := mgr.SaveVersionWithSensitivePaths(envSnapshot, metadata, sensitivePaths)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return version, nil
}

// RollbackToVersion validates and atomically publishes versionID as the current
// snapshot. The restored version contains materialized values, so
// RefreshSecrets is a no-op until a later source Reload reintroduces unresolved
// references. Source inspection attributes restored keys to the version record.
func (c *Config[T]) RollbackToVersion(versionID string) error {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.RollbackToVersionWithContext(ctx, versionID)
}

// RollbackToVersionWithContext is the context-aware form of
// [Config.RollbackToVersion]. It returns an error for a nil or canceled
// context, a frozen or closed Config, disabled versioning, an unknown ID, or a
// snapshot that fails current validation. Version storage may perform local
// filesystem I/O, but rollback never contacts configured source or secret
// providers. A successful rollback invokes change callbacks and emits
// "rollback" followed by "change" lifecycle events after publication.
func (c *Config[T]) RollbackToVersionWithContext(ctx context.Context, versionID string) error {
	if ctx == nil {
		return NewInvalidError("RollbackToVersion", versionID, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.RLock()
	if c.frozen {
		c.mu.RUnlock()
		return NewFrozenError("RollbackToVersion")
	}
	if c.closed {
		c.mu.RUnlock()
		return NewClosedError("RollbackToVersion")
	}
	mgr := c.versionMgr
	c.mu.RUnlock()
	if mgr == nil {
		return NewInvalidError("RollbackToVersion", versionID, errors.New("versioning not enabled"))
	}

	v := mgr.GetVersion(versionID)
	if v == nil {
		return &ConfigError{
			Op:   "RollbackToVersion",
			Key:  versionID,
			Code: ConfigErrorCodeNotFound,
			Err:  fmt.Errorf("version %s not found", versionID),
		}
	}

	snapshot := dictutil.DeepCopy(v.Config)
	if err := c.validateMaterializedCandidate(snapshot); err != nil {
		return err
	}
	tracker := sourcetrack.NewTracker(c.opts.DebugMode)
	tracker.TrackConfig(snapshot, "version:"+versionID, "version", c.env, "")

	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return err
	}
	if c.frozen {
		c.mu.Unlock()
		return NewFrozenError("RollbackToVersion")
	}
	if c.closed {
		c.mu.Unlock()
		return NewClosedError("RollbackToVersion")
	}
	before := dictutil.DeepCopy(c.envConfig)

	// A version stores the ready effective snapshot rather than source
	// references. Publish independent copies so subsequent mutation cannot alias
	// the retained version record or another live snapshot view. Because the
	// target is independent of current state, the write-lock acquisition is the
	// rollback's linearization point; unlike refresh, no revision retry is needed.
	c.envConfig = dictutil.DeepCopy(snapshot)
	c.unresolvedEnvConfig = dictutil.DeepCopy(snapshot)
	c.mergedConfig = dictutil.DeepCopy(snapshot)
	c.sensitivePaths = sensitivePathSet(v.SensitivePaths)
	c.sourceTracker.Restore(tracker.Snapshot())
	c.validatedModel = nil
	c.revision++
	after := dictutil.DeepCopy(c.envConfig)
	change := c.captureCommittedChange(before, after)
	c.mu.Unlock()

	c.deliverCommittedChange(ctx, change, nil, "rollback", versionID, dictutil.DeepCopy(after))
	return nil
}
