// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package sourcetrack

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"sync"

	"github.com/confiify/confii-go/v2/internal/sourcekind"
)

// FileTracker tracks file modification times and content hashes
// for incremental reload support.
//
// FileTracker distinguishes between trackable (real on-disk file) and
// untrackable (HTTP URL,
// environment-variable marker, cloud-object identifier, deleted/permission-
// denied path) sources. Untrackable sources are recorded in an internal
// set on first encounter and are reported as unchanged thereafter.
type FileTracker struct {
	mu          sync.RWMutex
	files       map[string]fileState
	untrackable map[string]struct{}
}

type fileState struct {
	mtime int64
	hash  string
}

// isNonFileScheme reports whether path looks like a non-file source
// based on a known scheme/marker prefix.
//
// The canonical scheme list lives in
// [github.com/confiify/confii-go/v2/internal/sourcekind]. This wrapper retains
// the FileTracker-local name (HasChanged/Track call sites) while consolidating
// the source of truth so a new scheme (e.g. a new cloud store) only needs to
// be added in one place. The predicate remains cheap (no regex, no URL parse)
// so it can be called inside HasChanged on every gate check.
func isNonFileScheme(path string) bool {
	return sourcekind.IsNonFileSource(path)
}

// NewFileTracker creates a new file tracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{
		files:       make(map[string]fileState),
		untrackable: make(map[string]struct{}),
	}
}

// Track starts tracking a file, recording its current mtime and hash.
//
// If path carries a non-file scheme (e.g. "http://", "environment:",
// "s3://"), Track classifies the source as untrackable, records it in
// the internal untrackable set, and returns nil. Subsequent HasChanged
// calls for that source return false instead of repeatedly reporting
// changed=true. For ordinary file paths, an os.Stat/read failure is surfaced as an
// error so initial wiring problems remain visible.
func (ft *FileTracker) Track(path string) error {
	if isNonFileScheme(path) {
		ft.mu.Lock()
		ft.untrackable[path] = struct{}{}
		ft.mu.Unlock()
		return nil
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()

	state, err := ft.readState(path)
	if err != nil {
		return err
	}
	ft.files[path] = state
	// A successful re-Track clears any stale untrackable classification:
	// the source has demonstrably become readable again.
	delete(ft.untrackable, path)
	return nil
}

// HasChanged returns true when the file's content hash differs from the
// last tracked state. mtime is retained as diagnostic metadata, but content
// is authoritative so a metadata-only touch does not trigger a reload.
//
// Unregistered local paths are treated as changed. Non-file and unreadable
// sources are classified as untrackable and return false until tracked again.
func (ft *FileTracker) HasChanged(path string) bool {
	ft.mu.RLock()
	if _, untrackable := ft.untrackable[path]; untrackable {
		ft.mu.RUnlock()
		return false
	}
	old, tracked := ft.files[path]
	ft.mu.RUnlock()

	if isNonFileScheme(path) {
		ft.mu.Lock()
		ft.untrackable[path] = struct{}{}
		ft.mu.Unlock()
		return false
	}

	if !tracked {
		return true
	}

	current, err := ft.readState(path)
	if err != nil {
		// Source was trackable at Track time but is now unreadable
		// (deleted, permission revoked). Promote to the untrackable
		// set so subsequent gate checks short-circuit to false rather
		// than spuriously reporting changed=true on every poll.
		ft.mu.Lock()
		ft.untrackable[path] = struct{}{}
		delete(ft.files, path)
		ft.mu.Unlock()
		return false
	}

	// Content is authoritative. mtime remains useful metadata and a cheap
	// first signal, but touching/copying an unchanged file must not trigger a
	// reload. The hash also catches edits on filesystems whose mtime did not
	// advance between rapid writes.
	return current.hash != old.hash
}

// IsTrackable reports whether the given source has been classified as
// trackable by the FileTracker. A source is trackable until either (a)
// it carries a non-file scheme prefix or (b) a Track / HasChanged call
// observed an os.Stat / read failure for it. The predicate is
// observation-driven: a path that has never been seen returns true
// (optimistically trackable) so callers can attempt Track without a
// pre-flight check.
func (ft *FileTracker) IsTrackable(path string) bool {
	if isNonFileScheme(path) {
		return false
	}
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	_, untrackable := ft.untrackable[path]
	return !untrackable
}

// Update updates the tracked state for a file.
func (ft *FileTracker) Update(path string) error {
	return ft.Track(path) // same operation
}

// GetChangedFiles returns the input paths whose content differs from their
// tracked state, preserving input order. Unregistered local paths are included;
// non-file and currently unreadable sources are excluded according to
// [FileTracker.HasChanged]. The method is safe for concurrent use and is useful
// to applications that coordinate reloads across a batch of dependent files.
func (ft *FileTracker) GetChangedFiles(paths []string) []string {
	var changed []string
	for _, p := range paths {
		if ft.HasChanged(p) {
			changed = append(changed, p)
		}
	}
	return changed
}

// Clear removes all tracked files. The untrackable classification set is
// also cleared so that previously unreadable sources get a fresh chance
// at classification, matching the "clean slate" intent of Clear.
func (ft *FileTracker) Clear() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.files = make(map[string]fileState)
	ft.untrackable = make(map[string]struct{})
}

// FileSnapshot is an opaque capture of a [FileTracker]'s state,
// produced by [FileTracker.Snapshot()] and consumed by
// [FileTracker.Restore]. The zero value is a valid empty snapshot;
// restoring it clears the tracker. Callers must not depend on its
// layout.
type FileSnapshot struct {
	files       map[string]fileState
	untrackable map[string]struct{}
}

// Snapshot returns a [FileSnapshot] of the tracker's current state.
// Subsequent Track / HasChanged / Clear calls on the live tracker do
// not affect the snapshot. fileState has no reference fields, so a
// map rebuild is structurally sufficient.
func (ft *FileTracker) Snapshot() FileSnapshot {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	files := make(map[string]fileState, len(ft.files))
	maps.Copy(files, ft.files)
	untrackable := make(map[string]struct{}, len(ft.untrackable))
	maps.Copy(untrackable, ft.untrackable)
	return FileSnapshot{files: files, untrackable: untrackable}
}

// Restore replaces the tracker's state with s. After this call, the
// tracker reports exactly the file states and untrackable
// classifications captured at the point [FileTracker.Snapshot()] was
// taken. Restore is goroutine-safe.
func (ft *FileTracker) Restore(s FileSnapshot) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	files := make(map[string]fileState, len(s.files))
	maps.Copy(files, s.files)
	untrackable := make(map[string]struct{}, len(s.untrackable))
	maps.Copy(untrackable, s.untrackable)
	ft.files = files
	ft.untrackable = untrackable
}

// osStat is a package-level indirection for os.Stat so tests can count
// invocations to preserve untrackable-set idempotency. Non-
// test code paths use os.Stat directly via this variable.
var osStat = os.Stat

func (ft *FileTracker) readState(path string) (fileState, error) {
	info, err := osStat(path)
	if err != nil {
		return fileState{}, err
	}

	// #nosec G304 -- path is a configuration source explicitly registered by the caller's loader.
	data, err := os.ReadFile(path)
	if err != nil {
		return fileState{}, err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	return fileState{
		mtime: info.ModTime().UnixNano(),
		hash:  hash,
	}, nil
}
