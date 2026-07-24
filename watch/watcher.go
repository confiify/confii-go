// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package watch provides file-watching capabilities for automatic config reloading.
package watch

import (
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/confiify/confii-go/internal/sourcekind"
	"github.com/fsnotify/fsnotify"
)

// ReloadFunc is called when a watched file changes.
type ReloadFunc func() error

// Watcher monitors configuration files and triggers reloads on changes.
type Watcher struct {
	watcher    *fsnotify.Watcher
	files      map[string]struct{} // absolute paths of watched files
	reloadFunc ReloadFunc
	logger     *slog.Logger
	done       chan struct{}
	once       sync.Once

	// mu guards 'present', which tracks whether each watched path
	// currently has a backing file on disk. This lets the loop
	// distinguish "rename/remove of a present file" from "create of a
	// previously removed file" so it can log once per state change and
	// re-arm reload after atomic-save sequences. The watch is on the
	// containing directory, so the directory-level fsnotify subscription
	// keeps delivering events across child rename/remove/recreate.
	mu      sync.Mutex
	present map[string]bool
}

// isNonFileSource reports whether s is a Loader.Source() identifier that
// cannot be watched via fsnotify (URL, env-prefix marker, cloud-store id).
//
// D-W20-01 (Wave 22): the canonical allowlist now lives in
// [github.com/confiify/confii-go/internal/sourcekind]; this thin wrapper
// preserves the watcher-local call site while eliminating the historical
// drift between watcher.go, sourcetrack/filetracker.go, and config.go.
func isNonFileSource(s string) bool {
	return sourcekind.IsNonFileSource(s)
}

// New creates a new file watcher.
//
// Sources that are not local file paths (HTTP/HTTPS URLs, "environment:"
// markers, cloud-store identifiers like "s3://", "gs://", "ssm:", etc.) are
// silently skipped with a warning log; only real file paths are registered
// with fsnotify. This lets callers pass the raw list of Loader.Source()
// strings without filtering by capability.
func New(files []string, reloadFunc ReloadFunc, logger *slog.Logger) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	w := &Watcher{
		watcher:    fw,
		files:      make(map[string]struct{}),
		reloadFunc: reloadFunc,
		logger:     logger,
		done:       make(chan struct{}),
		present:    make(map[string]bool),
	}

	// Deduplicate directories and register files.
	dirs := make(map[string]struct{})
	for _, f := range files {
		if isNonFileSource(f) {
			// Non-file sources cannot be watched via fsnotify; record
			// the skip so operators can see why a reload never fires
			// for that loader, but do not error out.
			logger.Warn("watch: skipping non-file source (not watchable)",
				slog.String("source", f))
			continue
		}
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		w.files[abs] = struct{}{}
		w.present[abs] = true
		dir := filepath.Dir(abs)
		dirs[dir] = struct{}{}
	}

	for dir := range dirs {
		if err := fw.Add(dir); err != nil {
			_ = fw.Close()
			return nil, err
		}
	}

	go w.loop()
	return w, nil
}

func (w *Watcher) loop() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			abs, _ := filepath.Abs(event.Name)
			if _, watched := w.files[abs]; !watched {
				continue
			}

			switch {
			case event.Op&fsnotify.Write != 0, event.Op&fsnotify.Create != 0:
				// Create can arrive on its own (atomic-save: tmpfile
				// rename produced a new inode at the watched path; or
				// remove+recreate). Write arrives for content changes.
				// Both should fire reload.
				w.mu.Lock()
				wasPresent := w.present[abs]
				w.present[abs] = true
				w.mu.Unlock()
				if !wasPresent && event.Op&fsnotify.Create != 0 {
					w.logger.Info("watched file recreated, re-arming",
						slog.String("file", event.Name))
				}
				w.logger.Info("config file changed, reloading",
					slog.String("file", event.Name))
				if err := w.reloadFunc(); err != nil {
					w.logger.Error("reload failed", slog.String("error", err.Error()))
				}

			case event.Op&fsnotify.Rename != 0:
				// Atomic-save case: original file was renamed away. The
				// directory watch survives, so a subsequent Create at
				// the same path will be observed. Mark not-present so
				// the next Create logs the re-arm.
				w.mu.Lock()
				w.present[abs] = false
				w.mu.Unlock()
				w.logger.Info("watched file renamed away; awaiting recreate",
					slog.String("file", event.Name))

			case event.Op&fsnotify.Remove != 0:
				// File unlinked. Don't crash, don't keep firing. The
				// directory watch survives so a future Create at the
				// same path WILL re-arm reload. Until then, sit quiet.
				w.mu.Lock()
				w.present[abs] = false
				w.mu.Unlock()
				w.logger.Info("watched file removed; suspending reloads until recreated",
					slog.String("file", event.Name))
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("file watcher error", slog.String("error", err.Error()))

		case <-w.done:
			return
		}
	}
}

// Stop stops the file watcher.
func (w *Watcher) Stop() {
	w.once.Do(func() {
		close(w.done)
		_ = w.watcher.Close()
	})
}
