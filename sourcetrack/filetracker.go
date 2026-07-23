package sourcetrack

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"sync"

	"github.com/confiify/confii-go/internal/sourcekind"
)

// FileTracker tracks file modification times and content hashes
// for incremental reload support.
//
// G20 (Wave 20) source-capability model: a FileTracker now distinguishes
// between trackable (real on-disk file) and untrackable (HTTP URL,
// environment-variable marker, cloud-object identifier, deleted/permission-
// denied path) sources. Untrackable sources are recorded in an internal
// set on first encounter and are reported as unchanged thereafter, so they
// no longer defeat the Wave 7 G14 incremental Reload optimization by
// returning "changed=true" on every gate check.
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
// D-W20-01 (Wave 22): the canonical scheme list now lives in
// [github.com/confiify/confii-go/internal/sourcekind]. This wrapper retains
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
// G20: if path carries a non-file scheme (e.g. "http://", "environment:",
// "s3://"), Track classifies the source as untrackable, records it in
// the internal untrackable set, and returns nil. Subsequent HasChanged
// calls for that source return false instead of repeatedly reporting
// changed=true. For ordinary file paths, Track keeps its pre-G20
// contract: an os.Stat / read failure is surfaced to the caller as an
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

// HasChanged returns true if the file's mtime or hash differs from the
// last tracked state.
//
// G20: HasChanged enforces the source-capability model. The decision
// tree is:
//
//  1. If the source is already classified as untrackable, return false
//     (no os.Stat call — idempotent fast path).
//  2. If the source has a non-file scheme prefix, classify it as
//     untrackable on the spot and return false.
//  3. If the source has not been registered with Track, fall back to
//     the pre-G20 "treat as changed" contract — callers (e.g. the Reload
//     incremental gate) re-Track such paths on the same pass.
//  4. Otherwise call readState. On a read failure (file deleted,
//     permission denied), classify as untrackable and return false; the
//     spurious-Reload loop that this sub-fix exists to kill is exactly
//     the case where step 4's read keeps failing on every gate check.
//  5. On a successful read, compare mtime+hash against the recorded
//     state — Wave 7 G14's incremental contract.
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

	return current.mtime != old.mtime || current.hash != old.hash
}

// IsTrackable reports whether the given source has been classified as
// trackable by the FileTracker. A source is trackable until either (a)
// it carries a non-file scheme prefix or (b) a Track / HasChanged call
// observed an os.Stat / read failure for it. The predicate is
// observation-driven: a path that has never been seen returns true
// (optimistically trackable) so callers can attempt Track without a
// pre-flight check.
//
// IsTrackable is additive (Wave 20, G20 sub-fix); no existing caller
// depends on it.
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

// GetChangedFiles returns which of the given files have changed.
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
// at classification — this matches the "clean slate" intent of the
// pre-G20 Clear contract.
func (ft *FileTracker) Clear() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.files = make(map[string]fileState)
	ft.untrackable = make(map[string]struct{})
}

// FileSnapshot is an opaque capture of a [FileTracker]'s state,
// produced by [FileTracker.Snapshot] and consumed by
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
// classifications captured at the point [FileTracker.Snapshot] was
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
// invocations to pin the G20 untrackable-set idempotency contract. Non-
// test code paths use os.Stat directly via this variable.
var osStat = os.Stat

func (ft *FileTracker) readState(path string) (fileState, error) {
	info, err := osStat(path)
	if err != nil {
		return fileState{}, err
	}

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
