// Package observe provides observability features: metrics, events, and versioning.
package observe

import (
	"sort"
	"sync"
	"time"
)

// AccessMetric tracks per-key access statistics.
type AccessMetric struct {
	AccessCount     int
	FirstAccess     time.Time
	LastAccess      time.Time
	TotalAccessTime time.Duration
}

// Metrics tracks overall configuration metrics.
type Metrics struct {
	mu              sync.RWMutex
	totalKeys       int
	accessMetrics   map[string]*AccessMetric
	reloadCount     int
	lastReload      time.Time
	reloadDurations []time.Duration
	changeCount     int
	lastChange      time.Time
	maxDurations    int
	enabled         bool
}

// NewMetrics creates a new metrics tracker.
func NewMetrics(totalKeys int) *Metrics {
	return &Metrics{
		totalKeys:     totalKeys,
		accessMetrics: make(map[string]*AccessMetric),
		maxDurations:  1000,
		enabled:       true,
	}
}

// RecordAccess records a key access with the given duration.
func (m *Metrics) RecordAccess(key string, duration time.Duration) {
	if !m.enabled {
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

// RecordReload records a reload event with duration.
func (m *Metrics) RecordReload(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadCount++
	m.lastReload = time.Now()
	m.reloadDurations = append(m.reloadDurations, duration)
	if len(m.reloadDurations) > m.maxDurations {
		m.reloadDurations = m.reloadDurations[1:]
	}
}

// RecordChange records a configuration change.
func (m *Metrics) RecordChange() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeCount++
	m.lastChange = time.Now()
}

// Statistics returns a summary of collected metrics.
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
		"total_keys":       m.totalKeys,
		"accessed_keys":    accessedKeys,
		"access_rate":      accessRate,
		"reload_count":     m.reloadCount,
		"avg_reload_time":  avgReload.String(),
		"change_count":     m.changeCount,
		"top_accessed_keys": top,
	}
	if !m.lastReload.IsZero() {
		stats["last_reload"] = m.lastReload
	}
	if !m.lastChange.IsZero() {
		stats["last_change"] = m.lastChange
	}
	return stats
}

// Enable starts collecting metrics.
func (m *Metrics) Enable() { m.enabled = true }

// Disable stops collecting metrics (retains existing data).
func (m *Metrics) Disable() { m.enabled = false }

// Reset clears all collected metrics.
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accessMetrics = make(map[string]*AccessMetric)
	m.reloadCount = 0
	m.reloadDurations = nil
	m.changeCount = 0
	m.lastReload = time.Time{}
	m.lastChange = time.Time{}
}
