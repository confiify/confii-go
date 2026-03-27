package sourcetrack

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
)

// FileTracker tracks file modification times and content hashes
// for incremental reload support.
type FileTracker struct {
	mu    sync.RWMutex
	files map[string]fileState
}

type fileState struct {
	mtime int64
	hash  string
}

// NewFileTracker creates a new file tracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{files: make(map[string]fileState)}
}

// Track starts tracking a file, recording its current mtime and hash.
func (ft *FileTracker) Track(path string) error {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	state, err := ft.readState(path)
	if err != nil {
		return err
	}
	ft.files[path] = state
	return nil
}

// HasChanged returns true if the file's mtime or hash differs from the last tracked state.
func (ft *FileTracker) HasChanged(path string) bool {
	ft.mu.RLock()
	old, tracked := ft.files[path]
	ft.mu.RUnlock()

	if !tracked {
		return true
	}

	current, err := ft.readState(path)
	if err != nil {
		return true // can't read → treat as changed
	}

	return current.mtime != old.mtime || current.hash != old.hash
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

// Clear removes all tracked files.
func (ft *FileTracker) Clear() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.files = make(map[string]fileState)
}

func (ft *FileTracker) readState(path string) (fileState, error) {
	info, err := os.Stat(path)
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
