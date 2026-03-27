// Package watch provides file-watching capabilities for automatic config reloading.
package watch

import (
	"log/slog"
	"path/filepath"
	"sync"

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
}

// New creates a new file watcher.
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
	}

	// Deduplicate directories and register files.
	dirs := make(map[string]struct{})
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		w.files[abs] = struct{}{}
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
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			abs, _ := filepath.Abs(event.Name)
			if _, watched := w.files[abs]; !watched {
				continue
			}
			w.logger.Info("config file changed, reloading", slog.String("file", event.Name))
			if err := w.reloadFunc(); err != nil {
				w.logger.Error("reload failed", slog.String("error", err.Error()))
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
