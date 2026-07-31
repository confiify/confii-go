// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package watch provides file-watching capabilities for automatic config reloading.
package watch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/confiify/confii-go/v2/internal/sourcekind"
	"github.com/fsnotify/fsnotify"
)

// ReloadFunc handles a watched-file creation or content change. Returning an
// error logs the failed reload and keeps the watcher running.
type ReloadFunc func() error

// ReloadFuncWithContext is called with the watcher's lifecycle context when a
// watched file changes.
type ReloadFuncWithContext func(context.Context) error

const defaultDebounce = 150 * time.Millisecond

// Option configures a Watcher before its filesystem subscriptions are
// installed.
type Option func(*options)

type options struct {
	debounce time.Duration
}

// WithDebounce sets the trailing-edge interval used to coalesce bursts of
// write and create events into one reload. Zero makes matching events reload
// immediately. Negative durations are rejected by [New] and [NewWithContext].
func WithDebounce(interval time.Duration) Option {
	return func(options *options) {
		options.debounce = interval
	}
}

// Watcher monitors configuration files and triggers reloads on changes.
type Watcher struct {
	watcher               *fsnotify.Watcher
	files                 map[string]struct{} // absolute paths of watched files
	dirs                  map[string]struct{} // directories registered with fsnotify
	reloadFunc            ReloadFunc
	reloadFuncWithContext ReloadFuncWithContext
	ctx                   context.Context
	cancel                context.CancelFunc
	logger                *slog.Logger
	debounce              time.Duration
	done                  chan struct{}
	once                  sync.Once

	// mu guards 'present', which tracks whether each watched path
	// currently has a backing file on disk. This lets the loop
	// distinguish removal from recreation so it can log once per state change and
	// re-arm reload after atomic-save sequences. The watch is on the
	// containing directory, so the directory-level fsnotify subscription
	// keeps delivering events across child rename/remove/recreate.
	mu      sync.Mutex
	present map[string]bool
}

// isNonFileSource reports whether s is a Loader.Source() identifier that
// cannot be watched via fsnotify (URL, env-prefix marker, cloud-store id).
//
// Classification is shared with source tracking through
// [github.com/confiify/confii-go/v2/internal/sourcekind].
func isNonFileSource(s string) bool {
	return sourcekind.IsNonFileSource(s)
}

// New creates a new file watcher.
//
// Sources that are not local file paths (HTTP/HTTPS URLs, "environment:"
// markers, cloud-store identifiers like "s3://", "gs://", "ssm:", etc.) are
// skipped with a warning log; only real file paths are registered
// with fsnotify. This lets callers pass the raw list of Loader.Source
// strings without filtering by capability.
func New(files []string, reloadFunc ReloadFunc, logger *slog.Logger, opts ...Option) (*Watcher, error) {
	return newWatcher(context.Background(), files, reloadFunc, nil, logger, opts...)
}

// NewWithContext creates a watcher whose loop and reload work are canceled when
// ctx is done. A nil context returns an error. A nil reload function is
// accepted but produces no action for matching events. Stop remains safe and
// idempotent.
func NewWithContext(ctx context.Context, files []string, reloadFunc ReloadFuncWithContext, logger *slog.Logger, opts ...Option) (*Watcher, error) {
	if ctx == nil {
		return nil, context.Canceled
	}
	return newWatcher(ctx, files, nil, reloadFunc, logger, opts...)
}

func newWatcher(ctx context.Context, files []string, reloadFunc ReloadFunc, reloadFuncWithContext ReloadFuncWithContext, logger *slog.Logger, opts ...Option) (*Watcher, error) {
	resolved := options{debounce: defaultDebounce}
	for index, option := range opts {
		if option == nil {
			return nil, fmt.Errorf("watch: option %d is nil", index)
		}
		if err := applyOption(&resolved, option); err != nil {
			return nil, fmt.Errorf("watch: apply option %d: %w", index, err)
		}
	}
	if resolved.debounce < 0 {
		return nil, errors.New("watch: debounce must not be negative")
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = slog.Default()
	}

	watchCtx, cancel := context.WithCancel(ctx)
	w := &Watcher{
		watcher:               fw,
		files:                 make(map[string]struct{}),
		dirs:                  make(map[string]struct{}),
		reloadFunc:            reloadFunc,
		reloadFuncWithContext: reloadFuncWithContext,
		ctx:                   watchCtx,
		cancel:                cancel,
		logger:                logger,
		debounce:              resolved.debounce,
		done:                  make(chan struct{}),
		present:               make(map[string]bool),
	}

	if err := w.ReplaceFiles(files); err != nil {
		cancel()
		_ = fw.Close()
		return nil, err
	}

	go w.loop()
	return w, nil
}

func applyOption(options *options, option Option) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("option panic: %v", recovered)
		}
	}()
	option(options)
	return nil
}

// ReplaceFiles atomically replaces the set of file paths that trigger reloads.
// Directory subscriptions already installed with fsnotify are retained so
// repeated dependency-graph changes do not churn operating-system handles.
// When a new directory cannot be watched, the prior file set remains active.
func (w *Watcher) ReplaceFiles(files []string) error {
	if w == nil {
		return nil
	}
	nextFiles := make(map[string]struct{})
	nextPresent := make(map[string]bool)
	nextDirs := make(map[string]struct{})
	for _, file := range files {
		if isNonFileSource(file) {
			w.logger.Warn("watch: skipping non-file source", "source", file)
			continue
		}
		abs, err := filepath.Abs(file)
		if err != nil {
			return err
		}
		nextFiles[abs] = struct{}{}
		_, statErr := os.Stat(abs)
		nextPresent[abs] = statErr == nil
		nextDirs[filepath.Dir(abs)] = struct{}{}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		return err
	}
	added := make([]string, 0)
	for dir := range nextDirs {
		if _, exists := w.dirs[dir]; exists {
			continue
		}
		if err := w.watcher.Add(dir); err != nil {
			for _, rollbackDir := range added {
				_ = w.watcher.Remove(rollbackDir)
			}
			return err
		}
		added = append(added, dir)
	}
	for _, dir := range added {
		w.dirs[dir] = struct{}{}
	}
	w.files = nextFiles
	w.present = nextPresent
	return nil
}

// Files returns the currently active absolute trigger paths in deterministic
// order. The returned slice is detached from watcher state.
func (w *Watcher) Files() []string {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	files := make([]string, 0, len(w.files))
	for file := range w.files {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func (w *Watcher) loop() {
	var debounceTimer *time.Timer
	var debounceC <-chan time.Time
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	runReload := func() {
		var err error
		if w.reloadFuncWithContext != nil {
			err = w.reloadFuncWithContext(w.ctx)
		} else if w.reloadFunc != nil {
			err = w.reloadFunc()
		}
		if err != nil && w.ctx.Err() == nil {
			w.logger.Error("reload failed", slog.String("error", err.Error()))
		}
	}
	scheduleReload := func() {
		if w.debounce == 0 {
			runReload()
			return
		}
		if debounceTimer == nil {
			debounceTimer = time.NewTimer(w.debounce)
			debounceC = debounceTimer.C
			return
		}
		if !debounceTimer.Stop() {
			select {
			case <-debounceTimer.C:
			default:
			}
		}
		debounceTimer.Reset(w.debounce)
		debounceC = debounceTimer.C
	}

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			abs, _ := filepath.Abs(event.Name)
			w.mu.Lock()
			_, watched := w.files[abs]
			w.mu.Unlock()
			if !watched {
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
				message := "config file changed, scheduling reload"
				if w.debounce == 0 {
					message = "config file changed, reloading"
				}
				w.logger.Info(message, slog.String("file", event.Name))
				scheduleReload()

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

		case <-debounceC:
			debounceC = nil
			runReload()

		case <-w.done:
			return
		case <-w.ctx.Done():
			return
		}
	}
}

// Stop cancels the watcher loop and releases its operating-system resources.
// It is safe to call more than once.
func (w *Watcher) Stop() {
	w.once.Do(func() {
		w.cancel()
		close(w.done)
		_ = w.watcher.Close()
	})
}
