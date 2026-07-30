// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Version represents a captured configuration snapshot. VersionManager stores
// an independent copy of Config; callers must treat values returned by manager
// methods as read-only.
//
// The Timestamp field carries sub-second precision (Unix seconds as a
// float64 with nanosecond fractional component) so callers can rely on
// strict monotonic ordering between snapshots taken in quick succession.
type Version struct {
	// VersionID is a stable 16-character hexadecimal identifier for this record.
	VersionID string `json:"version_id"`
	// Config is the materialized configuration captured by SaveVersion.
	Config map[string]any `json:"config"`
	// Timestamp is a strictly increasing Unix timestamp within one manager.
	Timestamp float64 `json:"timestamp"`
	// DateTime is Timestamp formatted as RFC3339Nano.
	DateTime string `json:"datetime"`
	// Metadata is caller-supplied descriptive data; it must be JSON-serializable
	// when disk persistence is enabled.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// VersionManager manages configuration version snapshots.
//
// When constructed with an empty storagePath the manager runs in-memory
// only and writes no source-tree artifacts. Callers that want persistent
// snapshots must pass an explicit on-disk directory.
type VersionManager struct {
	mu            sync.RWMutex
	storagePath   string
	maxVersions   int
	versions      map[string]*Version
	lastTS        int64   // monotonic counter (nanoseconds) used for IDs.
	lastTimestamp float64 // last externally exposed, strictly monotonic timestamp.
}

// NewVersionManager creates a new version manager.
//
// If storagePath is empty, snapshots remain in memory. Otherwise subsequent
// saves create the directory with mode 0700 and records with mode 0600. A
// non-positive maxVersions selects the default retention limit of 100.
func NewVersionManager(storagePath string, maxVersions int) *VersionManager {
	if maxVersions <= 0 {
		maxVersions = 100
	}
	return &VersionManager{
		storagePath: storagePath,
		maxVersions: maxVersions,
		versions:    make(map[string]*Version),
	}
}

// Reconfigure updates the manager's storage path and version cap in
// place. Already-captured snapshots are preserved; the new storage
// path applies to subsequent [VersionManager.SaveVersion] calls. If
// maxVersions <= 0, the existing cap is retained. The eviction
// policy reruns against the new cap so a tightened cap takes effect
// immediately.
func (m *VersionManager) Reconfigure(storagePath string, maxVersions int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storagePath = storagePath
	if maxVersions > 0 {
		m.maxVersions = maxVersions
	}
	m.evict()
}

// SaveVersion captures a snapshot of the configuration.
//
// Serialization failures are returned and no partial snapshot is persisted.
func (m *VersionManager) SaveVersion(config map[string]any, metadata map[string]any) (*Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Pick a strictly monotonic nanosecond timestamp. time.Now() can return
	// equal values when called in a tight loop on some platforms; bump by
	// one nanosecond if we ever observe a tie.
	nowNS := time.Now().UnixNano()
	if nowNS <= m.lastTS {
		nowNS = m.lastTS + 1
	}
	m.lastTS = nowNS
	now := time.Unix(0, nowNS)
	timestamp := float64(nowNS) / 1e9
	// A float64 at the current Unix epoch cannot represent every nanosecond.
	// Advance to the next representable value when Windows' lower-resolution
	// clock (or a rapid save) would otherwise expose an equal timestamp.
	if timestamp <= m.lastTimestamp {
		timestamp = math.Nextafter(m.lastTimestamp, math.Inf(1))
	}
	m.lastTimestamp = timestamp

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("version: marshal config: %w", err)
	}
	hash := sha256.Sum256(append(configJSON, []byte(fmt.Sprintf("%d", nowNS))...))
	versionID := fmt.Sprintf("%x", hash[:8])

	// Deep copy via JSON round-trip to ensure the snapshot is immutable.
	var configCopy map[string]any
	if err := json.Unmarshal(configJSON, &configCopy); err != nil {
		return nil, fmt.Errorf("version: unmarshal snapshot: %w", err)
	}

	v := &Version{
		VersionID: versionID,
		Config:    configCopy,
		Timestamp: timestamp,
		DateTime:  now.Format(time.RFC3339Nano),
		Metadata:  metadata,
	}

	// Persist to disk only when an explicit storage path was supplied.
	if m.storagePath != "" {
		if err := os.MkdirAll(m.storagePath, 0700); err != nil {
			return nil, fmt.Errorf("version: mkdir storage: %w", err)
		}
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("version: marshal version record: %w", err)
		}
		path := filepath.Join(m.storagePath, versionID+".json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			return nil, fmt.Errorf("version: write snapshot: %w", err)
		}
	}

	m.versions[versionID] = v
	m.evict()

	return v, nil
}

// GetVersion retrieves id from memory or configured disk storage. It returns
// nil for an unknown or invalid ID, unreadable storage, or malformed record.
// The returned record is manager-owned and must be treated as read-only.
func (m *VersionManager) GetVersion(id string) *Version {
	m.mu.RLock()
	if v, ok := m.versions[id]; ok {
		m.mu.RUnlock()
		return v
	}
	m.mu.RUnlock()

	if m.storagePath == "" {
		return nil
	}
	if !validVersionID(id) {
		return nil
	}

	// OpenRoot keeps a malicious symlink inside the snapshot directory from
	// redirecting reads outside that directory.
	root, err := os.OpenRoot(m.storagePath)
	if err != nil {
		return nil
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(id + ".json")
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	var v Version
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return &v
}

// ListVersions returns all known versions sorted by timestamp (newest first).
//
// The returned slice may be reordered without affecting the manager, but its
// Version elements are manager-owned and must be treated as read-only. Invalid
// or unreadable disk records are skipped.
func (m *VersionManager) ListVersions() []*Version {
	m.mu.Lock()
	m.scanDiskLocked()
	versions := make([]*Version, 0, len(m.versions))
	for _, v := range m.versions {
		versions = append(versions, v)
	}
	m.mu.Unlock()

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Timestamp > versions[j].Timestamp
	})
	return versions
}

// LatestVersion returns the most recent known version, or nil when none exists.
// The returned record is manager-owned and must be treated as read-only.
func (m *VersionManager) LatestVersion() *Version {
	versions := m.ListVersions()
	if len(versions) == 0 {
		return nil
	}
	return versions[0]
}

// DiffVersions compares two version snapshots and returns a list of
// differences. Each element is a map with keys: "path", "type", and one or
// both of "old_value"/"new_value".
func (m *VersionManager) DiffVersions(id1, id2 string) ([]map[string]any, error) {
	v1 := m.GetVersion(id1)
	if v1 == nil {
		return nil, fmt.Errorf("version %s not found", id1)
	}
	v2 := m.GetVersion(id2)
	if v2 == nil {
		return nil, fmt.Errorf("version %s not found", id2)
	}
	return versionDiffMaps(v1.Config, v2.Config, ""), nil
}

func versionDiffMaps(a, b map[string]any, prefix string) []map[string]any {
	var diffs []map[string]any
	keys := make(map[string]struct{})
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	for k := range keys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		va, inA := a[k]
		vb, inB := b[k]
		switch {
		case !inA:
			diffs = append(diffs, map[string]any{"path": path, "type": "added", "new_value": vb})
		case !inB:
			diffs = append(diffs, map[string]any{"path": path, "type": "removed", "old_value": va})
		default:
			ma, aMap := va.(map[string]any)
			mb, bMap := vb.(map[string]any)
			if aMap && bMap {
				diffs = append(diffs, versionDiffMaps(ma, mb, path)...)
			} else {
				ja, _ := json.Marshal(va)
				jb, _ := json.Marshal(vb)
				if string(ja) != string(jb) {
					diffs = append(diffs, map[string]any{"path": path, "type": "modified", "old_value": va, "new_value": vb})
				}
			}
		}
	}
	return diffs
}

// scanDiskLocked loads any version files present on disk that are not
// already cached in memory. Caller must hold the write lock.
func (m *VersionManager) scanDiskLocked() {
	if m.storagePath == "" {
		return
	}
	entries, err := os.ReadDir(m.storagePath)
	if err != nil {
		return
	}
	root, err := os.OpenRoot(m.storagePath)
	if err != nil {
		return
	}
	defer func() { _ = root.Close() }()
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-5]
		if !validVersionID(id) {
			continue
		}
		if _, ok := m.versions[id]; ok {
			continue
		}
		f, err := root.Open(entry.Name())
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			continue
		}
		var v Version
		if err := json.Unmarshal(data, &v); err != nil {
			continue
		}
		if v.VersionID != id {
			continue
		}
		m.versions[id] = &v
	}
}

func validVersionID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (m *VersionManager) evict() {
	if len(m.versions) <= m.maxVersions {
		return
	}
	// Sort by timestamp, remove oldest.
	type entry struct {
		id string
		ts float64
	}
	var entries []entry
	for id, v := range m.versions {
		entries = append(entries, entry{id, v.Timestamp})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ts < entries[j].ts })

	for len(entries) > m.maxVersions {
		oldest := entries[0]
		entries = entries[1:]
		delete(m.versions, oldest.id)
		if m.storagePath != "" {
			_ = os.Remove(filepath.Join(m.storagePath, oldest.id+".json"))
		}
	}
}
