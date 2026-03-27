package observe

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Version represents an immutable configuration snapshot.
type Version struct {
	VersionID  string         `json:"version_id"`
	Config     map[string]any `json:"config"`
	Timestamp  float64        `json:"timestamp"`
	DateTime   string         `json:"datetime"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// VersionManager manages configuration version snapshots.
type VersionManager struct {
	mu          sync.RWMutex
	storagePath string
	maxVersions int
	versions    map[string]*Version
}

// NewVersionManager creates a new version manager.
func NewVersionManager(storagePath string, maxVersions int) *VersionManager {
	if storagePath == "" {
		storagePath = ".confii/versions"
	}
	if maxVersions <= 0 {
		maxVersions = 100
	}
	return &VersionManager{
		storagePath: storagePath,
		maxVersions: maxVersions,
		versions:    make(map[string]*Version),
	}
}

// SaveVersion captures a snapshot of the configuration.
func (m *VersionManager) SaveVersion(config map[string]any, metadata map[string]any) (*Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	configJSON, _ := json.Marshal(config)
	hash := sha256.Sum256(append(configJSON, []byte(fmt.Sprintf("%d", now.UnixNano()))...))
	versionID := fmt.Sprintf("%x", hash[:8])

	// Deep copy via JSON round-trip to ensure snapshot is immutable.
	var configCopy map[string]any
	json.Unmarshal(configJSON, &configCopy)

	v := &Version{
		VersionID: versionID,
		Config:    configCopy,
		Timestamp: float64(now.Unix()),
		DateTime:  now.Format(time.RFC3339),
		Metadata:  metadata,
	}

	// Persist to disk.
	if err := os.MkdirAll(m.storagePath, 0755); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(m.storagePath, versionID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, err
	}

	m.versions[versionID] = v
	m.evict()

	return v, nil
}

// GetVersion retrieves a version by ID.
func (m *VersionManager) GetVersion(id string) *Version {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if v, ok := m.versions[id]; ok {
		return v
	}

	// Try loading from disk.
	path := filepath.Join(m.storagePath, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v Version
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return &v
}

// ListVersions returns all versions sorted by timestamp (newest first).
func (m *VersionManager) ListVersions() []*Version {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Scan disk for any we haven't loaded.
	m.scanDisk()

	versions := make([]*Version, 0, len(m.versions))
	for _, v := range m.versions {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Timestamp > versions[j].Timestamp
	})
	return versions
}

// LatestVersion returns the most recent version.
func (m *VersionManager) LatestVersion() *Version {
	versions := m.ListVersions()
	if len(versions) == 0 {
		return nil
	}
	return versions[0]
}

// DiffVersions compares two version snapshots and returns a list of differences.
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

func (m *VersionManager) scanDisk() {
	entries, err := os.ReadDir(m.storagePath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-5]
		if _, ok := m.versions[id]; ok {
			continue
		}
		path := filepath.Join(m.storagePath, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var v Version
		if err := json.Unmarshal(data, &v); err != nil {
			continue
		}
		m.versions[id] = &v
	}
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
		os.Remove(filepath.Join(m.storagePath, oldest.id+".json"))
	}
}
