package confii

import (
	"context"
	"github.com/confiify/confii-go/internal/sourcekind"
	"github.com/confiify/confii-go/watch"
	"log/slog"
)

// Freeze makes the config immutable.
func (c *Config[T]) Freeze() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frozen = true
}

// IsFrozen returns whether the config is frozen.
func (c *Config[T]) IsFrozen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.frozen
}

// Env returns the active environment name.
func (c *Config[T]) Env() string { return c.env }

// StopWatching stops the file watcher if running.
func (c *Config[T]) StopWatching() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watcher != nil {
		c.watcher.Stop()
		c.watcher = nil
	}
}

func (c *Config[T]) startWatching() {
	// G20-residual (Wave 21): filter non-file loader sources at the call
	// site before handing them to the watch package. Wave 20 Fixer-AT
	// added a defensive scheme allowlist inside watch.New that emits a
	// "skipping non-file source" warning on every reload — useful as a
	// safety net, but noisy when the call site can trivially pre-filter.
	// We duplicate the small scheme-prefix check here (D-W20-01 tracks
	// extracting it into a shared internal package next wave) so loaders
	// like envPrefixAutoLoader ("environment:APP"), HTTPLoader
	// ("http(s)://..."), and cloud-store loaders ("s3://", "ssm:",
	// "gs://", "azure://", "ibmcos://", "git:", "consul://", "vault:")
	// never reach watch.New on the happy path.
	var files []string
	for _, l := range c.loaders {
		src := l.Source()
		if isNonFileLoaderSource(src) {
			continue
		}
		files = append(files, src)
	}
	if len(files) == 0 {
		// All loader sources are non-file (e.g. env-only configs).
		// Skip watcher construction entirely so we don't hold an
		// fsnotify handle for nothing.
		return
	}
	w, err := watch.New(files, func() error {
		return c.Reload(context.Background())
	}, c.logger)
	if err != nil {
		c.logger.Warn("failed to start file watcher", slog.String("error", err.Error()))
		return
	}
	c.watcher = w
}

// isNonFileLoaderSource reports whether a Loader.Source() identifier is one
// of the URL-style or marker-prefix forms whose backing storage cannot be
// observed via fsnotify.
//
// D-W20-01 (Wave 22): the canonical allowlist is now owned by
// [github.com/confiify/confii-go/internal/sourcekind]. This wrapper preserves
// the local call-site name; the three formerly-divergent lists (watch,
// sourcetrack, confii) all share the consolidated predicate.
func isNonFileLoaderSource(s string) bool {
	return sourcekind.IsNonFileSource(s)
}
