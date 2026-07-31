// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package observe provides observability features: metrics, events, and versioning.
package observe

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// AccessMetric is a point-in-time per-key access summary.
type AccessMetric struct {
	// AccessCount is the number of recorded reads.
	AccessCount int
	// FirstAccess is the timestamp of the first recorded read.
	FirstAccess time.Time
	// LastAccess is the timestamp of the most recent recorded read.
	LastAccess time.Time
	// TotalAccessTime is the sum of recorded read durations.
	TotalAccessTime time.Duration
}

// Metrics collects bounded, in-process configuration counters and timings. It
// is safe for concurrent use. Metrics are process-local and are not exported to
// an external telemetry backend automatically.
type Metrics struct {
	mu                sync.RWMutex
	totalKeys         int
	accessMetrics     map[string]*AccessMetric
	reloadCount       int
	lastReload        time.Time
	reloadDurations   []time.Duration
	reloadFailedCount int
	lastReloadFailed  time.Time
	extendCount       int
	lastExtend        time.Time
	extendFailedCount int
	lastExtendFailed  time.Time
	// Mutation counters distinguish published changes from rejected operations.
	setCount              int
	lastSet               time.Time
	setFailedCount        int
	lastSetFailed         time.Time
	overrideCount         int
	lastOverride          time.Time
	overrideFailedCount   int
	lastOverrideFailed    time.Time
	overrideRestoredCount int
	lastOverrideRestored  time.Time
	changeCount           int
	lastChange            time.Time
	maxDurations          int
	// enabled gates every recording method and is read/written atomically.
	// Use enabled.Load() / enabled.Store(...) — never bare reads or
	// writes — so the race detector stays quiet under concurrent
	// Enable/Disable + RecordAccess traffic.
	enabled atomic.Bool
}

// NewMetrics creates a new metrics tracker.
//
// The returned Metrics is enabled by default; callers may toggle the
// state with [Metrics.Enable] / [Metrics.Disable] from any goroutine.
func NewMetrics(totalKeys int) *Metrics {
	m := &Metrics{
		totalKeys:     totalKeys,
		accessMetrics: make(map[string]*AccessMetric),
		maxDurations:  1000,
	}
	m.enabled.Store(true)
	return m
}

// RecordAccess records one successful read of key and its duration. It is a
// no-op while collection is disabled.
func (m *Metrics) RecordAccess(key string, duration time.Duration) {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	am, ok := m.accessMetrics[key]
	if !ok {
		am = &AccessMetric{FirstAccess: time.Now()}
		m.accessMetrics[key] = am
	}
	am.AccessCount++
	am.LastAccess = time.Now()
	am.TotalAccessTime += duration
}

// RecordReload records one successfully committed reload and its duration.
// Use [Metrics.RecordReloadFailed] for rejected candidates.
func (m *Metrics) RecordReload(duration time.Duration) {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadCount++
	m.lastReload = time.Now()
	m.reloadDurations = append(m.reloadDurations, duration)
	if len(m.reloadDurations) > m.maxDurations {
		m.reloadDurations = m.reloadDurations[1:]
	}
}

// RecordReloadFailed records a reload attempt that did not commit.
//
// This lets observability distinguish a successful reload from a candidate
// rejected before publication. The duration is the wall-clock time spent
// inside [confii.Config.Reload] preparing and rejecting that candidate. The
// reloadCount counter is left unchanged — callers querying
// [Metrics.Statistics] see only successful reloads in reload_count and
// rejected candidates in reload_failed_count.
func (m *Metrics) RecordReloadFailed(duration time.Duration) {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadFailedCount++
	m.lastReloadFailed = time.Now()
	_ = duration // duration is reserved for future histogram support.
}

// RecordExtend records a successful runtime extension that committed a
// new loader's data into the live configuration.
//
// The counter is incremented only after an extension commits. Failed
// extensions are recorded by [Metrics.RecordExtendFailed]. Duration is the
// wall-clock time from the start of Extend through publication.
func (m *Metrics) RecordExtend(duration time.Duration) {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extendCount++
	m.lastExtend = time.Now()
	_ = duration // duration is reserved for future histogram support.
}

// RecordExtendFailed records an Extend attempt that did not commit.
//
// This lets observability distinguish a successful runtime extension from a
// candidate rejected before publication. Failures may originate in the loader
// itself (under ErrorPolicyRaise), composition, materialization, or validation.
// extendCount stays unchanged so querying [Metrics.Statistics] separately
// reports committed extensions in extend_count and rejected candidates in
// extend_failed_count.
func (m *Metrics) RecordExtendFailed(duration time.Duration) {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.extendFailedCount++
	m.lastExtendFailed = time.Now()
	_ = duration // duration is reserved for future histogram support.
}

// RecordChange records a configuration change.
func (m *Metrics) RecordChange() {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeCount++
	m.lastChange = time.Now()
}

// RecordSet records a successful runtime [confii.Config.Set] mutation
// that committed a new value into the live configuration.
//
// Like [Metrics.RecordReload] and [Metrics.RecordExtend], this counter
// is incremented only on a *committed* Set; calls that returned a
// path-traversal error before mutating live state are reported via
// [Metrics.RecordSetFailed].
func (m *Metrics) RecordSet() {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setCount++
	m.lastSet = time.Now()
}

// RecordSetFailed records a Set attempt that did not commit,
// typically because the requested key path traversed through a
// non-map intermediate. setCount stays unchanged so [Metrics.Statistics]
// reports committed Set calls in set_count and rejected ones in
// set_failed_count.
func (m *Metrics) RecordSetFailed() {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setFailedCount++
	m.lastSetFailed = time.Now()
}

// RecordOverride records a successful runtime [confii.Config.Override]
// invocation that committed an override payload into the live
// configuration. Failed Override calls (path errors during
// SetNested) are reported via [Metrics.RecordOverrideFailed].
func (m *Metrics) RecordOverride() {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overrideCount++
	m.lastOverride = time.Now()
}

// RecordOverrideFailed records an Override attempt that did not commit
// typically because one of the override keys traversed through
// a non-map intermediate. overrideCount stays unchanged so
// [Metrics.Statistics] reports committed Overrides in override_count
// and rejected ones in override_failed_count.
func (m *Metrics) RecordOverrideFailed() {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overrideFailedCount++
	m.lastOverrideFailed = time.Now()
}

// RecordOverrideRestored records a successful invocation of the
// restore function returned by [confii.Config.Override]. Pairing
// override / override_restored counters lets observers detect
// Override leaks where a restore was never invoked.
func (m *Metrics) RecordOverrideRestored() {
	if !m.enabled.Load() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overrideRestoredCount++
	m.lastOverrideRestored = time.Now()
}

// Statistics returns a detached map containing operation counters, last-event
// timestamps, average reload time, access rate, and up to ten most-accessed
// keys. Mutating the returned map does not alter the collector.
func (m *Metrics) Statistics() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	accessedKeys := len(m.accessMetrics)
	accessRate := float64(0)
	if m.totalKeys > 0 {
		accessRate = float64(accessedKeys) / float64(m.totalKeys)
	}

	var avgReload time.Duration
	if len(m.reloadDurations) > 0 {
		var total time.Duration
		for _, d := range m.reloadDurations {
			total += d
		}
		avgReload = total / time.Duration(len(m.reloadDurations))
	}

	// Top 10 accessed keys.
	type kv struct {
		key   string
		count int
	}
	var topKeys []kv
	for k, am := range m.accessMetrics {
		topKeys = append(topKeys, kv{k, am.AccessCount})
	}
	sort.Slice(topKeys, func(i, j int) bool { return topKeys[i].count > topKeys[j].count })
	if len(topKeys) > 10 {
		topKeys = topKeys[:10]
	}
	top := make(map[string]int, len(topKeys))
	for _, kv := range topKeys {
		top[kv.key] = kv.count
	}

	stats := map[string]any{
		"total_keys":              m.totalKeys,
		"accessed_keys":           accessedKeys,
		"access_rate":             accessRate,
		"reload_count":            m.reloadCount,
		"reload_failed_count":     m.reloadFailedCount,
		"avg_reload_time":         avgReload.String(),
		"extend_count":            m.extendCount,
		"extend_failed_count":     m.extendFailedCount,
		"set_count":               m.setCount,
		"set_failed_count":        m.setFailedCount,
		"override_count":          m.overrideCount,
		"override_failed_count":   m.overrideFailedCount,
		"override_restored_count": m.overrideRestoredCount,
		"change_count":            m.changeCount,
		"top_accessed_keys":       top,
	}
	if !m.lastReload.IsZero() {
		stats["last_reload"] = m.lastReload
	}
	if !m.lastReloadFailed.IsZero() {
		stats["last_reload_failed"] = m.lastReloadFailed
	}
	if !m.lastExtend.IsZero() {
		stats["last_extend"] = m.lastExtend
	}
	if !m.lastExtendFailed.IsZero() {
		stats["last_extend_failed"] = m.lastExtendFailed
	}
	if !m.lastSet.IsZero() {
		stats["last_set"] = m.lastSet
	}
	if !m.lastSetFailed.IsZero() {
		stats["last_set_failed"] = m.lastSetFailed
	}
	if !m.lastOverride.IsZero() {
		stats["last_override"] = m.lastOverride
	}
	if !m.lastOverrideFailed.IsZero() {
		stats["last_override_failed"] = m.lastOverrideFailed
	}
	if !m.lastOverrideRestored.IsZero() {
		stats["last_override_restored"] = m.lastOverrideRestored
	}
	if !m.lastChange.IsZero() {
		stats["last_change"] = m.lastChange
	}
	return stats
}

// Enable starts collecting metrics.
//
// Safe for concurrent use without external synchronization. The flag
// is flipped via a single atomic store, so a concurrent
// [Metrics.RecordAccess] call will either observe the previous value
// or the new one without producing a data race.
func (m *Metrics) Enable() { m.enabled.Store(true) }

// Disable stops collecting metrics (retains existing data).
//
// Safe for concurrent use without external synchronization. After
// Disable returns, every later [Metrics.RecordAccess] invocation in
// the calling goroutine will observe the disabled state and
// short-circuit; concurrent recorders started before the Disable call
// may still complete an in-flight update because they had already
// passed the atomic gate.
func (m *Metrics) Disable() { m.enabled.Store(false) }

// Reset clears counters, timings, and access history without changing whether
// collection is enabled or the configured total key count.
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accessMetrics = make(map[string]*AccessMetric)
	m.reloadCount = 0
	m.reloadDurations = nil
	m.reloadFailedCount = 0
	m.extendCount = 0
	m.extendFailedCount = 0
	m.setCount = 0
	m.setFailedCount = 0
	m.overrideCount = 0
	m.overrideFailedCount = 0
	m.overrideRestoredCount = 0
	m.changeCount = 0
	m.lastReload = time.Time{}
	m.lastReloadFailed = time.Time{}
	m.lastExtend = time.Time{}
	m.lastExtendFailed = time.Time{}
	m.lastSet = time.Time{}
	m.lastSetFailed = time.Time{}
	m.lastOverride = time.Time{}
	m.lastOverrideFailed = time.Time{}
	m.lastOverrideRestored = time.Time{}
	m.lastChange = time.Time{}
}
