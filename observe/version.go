// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package observe

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/confiify/confii-go/v2/diff"
	"github.com/google/renameio/v2/maybe"
	"github.com/oklog/ulid/v2"
)

// Version represents a captured configuration snapshot. VersionManager stores
// an independent copy of Config; callers must treat values returned by manager
// methods as read-only.
//
// Timestamp records when the snapshot was created. VersionID is a monotonic
// ULID and provides deterministic ordering when multiple snapshots share the
// same clock instant.
type Version struct {
	// VersionID is a canonical, time-sortable ULID for this record.
	VersionID string `json:"version_id"`
	// Config is the materialized configuration captured by SaveVersion.
	Config map[string]any `json:"config"`
	// Timestamp is the UTC creation time serialized as RFC3339Nano in JSON.
	Timestamp time.Time `json:"timestamp"`
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
	lastTimestamp time.Time
	entropy       io.Reader
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
		entropy:     ulid.Monotonic(rand.Reader, 0),
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

	now := time.Now().UTC()
	if !now.After(m.lastTimestamp) {
		now = m.lastTimestamp.Add(time.Nanosecond)
	}
	m.lastTimestamp = now

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("version: marshal config: %w", err)
	}
	identifier, err := ulid.New(ulid.Timestamp(now), m.entropy)
	if err != nil {
		return nil, fmt.Errorf("version: generate ULID: %w", err)
	}
	versionID := identifier.String()

	// Deep copy via JSON round-trip to ensure the snapshot is immutable.
	var configCopy map[string]any
	if err := json.Unmarshal(configJSON, &configCopy); err != nil {
		return nil, fmt.Errorf("version: unmarshal snapshot: %w", err)
	}
	var metadataCopy map[string]any
	if metadata != nil {
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			return nil, fmt.Errorf("version: marshal metadata: %w", err)
		}
		if err := json.Unmarshal(metadataJSON, &metadataCopy); err != nil {
			return nil, fmt.Errorf("version: unmarshal metadata snapshot: %w", err)
		}
	}

	v := &Version{
		VersionID: versionID,
		Config:    configCopy,
		Timestamp: now,
		Metadata:  metadataCopy,
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
		if err := maybe.WriteFile(path, data, 0600); err != nil {
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
		if versions[i].Timestamp.Equal(versions[j].Timestamp) {
			return versions[i].VersionID > versions[j].VersionID
		}
		return versions[i].Timestamp.After(versions[j].Timestamp)
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
func (m *VersionManager) DiffVersions(id1, id2 string) ([]diff.ConfigDiff, error) {
	v1 := m.GetVersion(id1)
	if v1 == nil {
		return nil, fmt.Errorf("version %s not found", id1)
	}
	v2 := m.GetVersion(id2)
	if v2 == nil {
		return nil, fmt.Errorf("version %s not found", id2)
	}
	return diff.Diff(v1.Config, v2.Config), nil
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
	parsed, err := ulid.ParseStrict(id)
	return err == nil && parsed.String() == id
}

func (m *VersionManager) evict() {
	if len(m.versions) <= m.maxVersions {
		return
	}
	// Sort by timestamp, remove oldest.
	type entry struct {
		id string
		ts time.Time
	}
	var entries []entry
	for id, v := range m.versions {
		entries = append(entries, entry{id, v.Timestamp})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ts.Equal(entries[j].ts) {
			return entries[i].id < entries[j].id
		}
		return entries[i].ts.Before(entries[j].ts)
	})

	for len(entries) > m.maxVersions {
		oldest := entries[0]
		entries = entries[1:]
		delete(m.versions, oldest.id)
		if m.storagePath != "" {
			_ = os.Remove(filepath.Join(m.storagePath, oldest.id+".json"))
		}
	}
}
