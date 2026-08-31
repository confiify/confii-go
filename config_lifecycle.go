// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"sort"

	"github.com/confiify/confii-go/v2/internal/sourcekind"
	"github.com/confiify/confii-go/v2/watch"
)

// Freeze prevents subsequent mutation through Set, Extend, Reload,
// RefreshSecrets, and version rollback. Reads remain available. Override is a
// scoped exception: it temporarily makes the Config mutable and restores the
// prior frozen state when its last restore function is called. Freeze is
// idempotent and safe for concurrent use.
func (c *Config[T]) Freeze() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frozen = true
}

// IsFrozen reports whether ordinary mutation operations are currently
// disabled. An active Override scope may temporarily change the result.
func (c *Config[T]) IsFrozen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.frozen
}

// Env returns the environment selected when the current snapshot was built.
// The value is empty when no environment was selected.
func (c *Config[T]) Env() string { return c.env }

// StopWatching stops automatic file-backed reloads, if enabled. It is
// idempotent; explicit Reload calls remain available.
func (c *Config[T]) StopWatching() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watchCancel != nil {
		c.watchCancel()
		c.watchCancel = nil
	}
	if c.watcher != nil {
		c.watcher.Stop()
		c.watcher = nil
	}
}

// Close stops background watchers and closes loaders, secret resolvers, and
// other managed resources that implement interface{ Close() error }. It is
// idempotent and safe for concurrent use. A closed Config remains readable as
// its final immutable snapshot, while mutation methods return
// [ErrConfigClosed]. Errors from all closable resources are combined and can
// be inspected with [errors.Is] or [errors.As].
//
// A managed resource's Close must not call back into this Config's Close.
// Disposal runs inside a [sync.Once], which is not re-entrant, so a resource
// that closes its owner deadlocks the goroutine rather than returning an error.
// Nothing Confii ships does this; the constraint is stated because Close
// invokes consumer-supplied code and the failure is silent when violated.
//
// Close does not erase what the configuration holds. The published snapshot
// stays readable, resolved secrets included; see docs/ownership.md for what
// this bounds and what it does not.
func (c *Config[T]) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		if c.watchCancel != nil {
			c.watchCancel()
			c.watchCancel = nil
		}
		if c.watcher != nil {
			c.watcher.Stop()
			c.watcher = nil
		}
		resources := make([]any, 0, len(c.loaders)+1)
		for _, loader := range c.loaders {
			resources = append(resources, loader)
		}
		resources = append(resources, c.opts.SecretResolver)
		registry := c.resourceRegistry
		c.mu.Unlock()
		if registry != nil {
			resources = append(resources, registry.closeAndSnapshot()...)
		}
		// Deliberately no ClearCache fallback for a resolver that has no Close.
		// ClearCache is an operational call — it is what RefreshSecrets uses to
		// make the next resolution consult the store — not a disposal call, and
		// implementations treat it as live: one is free to run arbitrary work
		// there, including work that closes this configuration, which would
		// re-enter closeOnce on this goroutine and deadlock. It would also buy
		// little. The materialized configuration keeps its resolved values
		// after Close by design, so clearing a resolver cache while that copy
		// remains does not make the process forget the secret. A resolver
		// owning material that must not outlive shutdown implements
		// CloseableSecretResolver, which is what that interface is for.
		for _, resource := range resources {
			if closer, ok := resource.(interface{ Close() error }); ok {
				closeErr = errors.Join(closeErr, closer.Close())
			}
		}
	})
	return closeErr
}

func (c *Config[T]) startWatching() error {
	// Filter non-file loader sources before handing them to the watch package.
	// The canonical predicate lives in internal/sourcekind, so loaders
	// like envPrefixAutoLoader ("environment:APP"), HTTPLoader
	// ("http(s)://..."), and cloud-store loaders ("s3://", "ssm:",
	// "gs://", "azure://", "ibmcos://", "git:", "consul://", "vault:")
	// never reach watch.New on the happy path.
	files := c.watchedFiles()
	if len(files) == 0 {
		// All loader sources are non-file (e.g. env-only configs).
		// Skip watcher construction entirely so we don't hold an
		// fsnotify handle for nothing.
		return nil
	}
	watchCtx, cancel := context.WithCancel(context.Background())
	w, err := watch.NewWithContext(watchCtx, files, func(ctx context.Context) error {
		if c.opts.OperationTimeout > 0 {
			operationCtx, operationCancel := context.WithTimeout(ctx, c.opts.OperationTimeout)
			defer operationCancel()
			return c.ReloadWithContext(operationCtx)
		}
		return c.ReloadWithContext(ctx)
	}, c.logger, watch.WithDebounce(c.opts.ReloadDebounce))
	if err != nil {
		cancel()
		return err
	}
	c.watchCancel = cancel
	c.watcher = w
	return nil
}

func (c *Config[T]) watchedFiles() []string {
	seen := make(map[string]struct{})
	files := make([]string, 0, len(c.loaders))
	add := func(path string) {
		if path == "" || isNonFileLoaderSource(path) {
			return
		}
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	for _, loader := range c.loaders {
		add(loader.Source())
	}
	for _, dependencies := range c.loaderDependencies {
		for _, dependency := range dependencies {
			add(dependency)
		}
	}
	sort.Strings(files)
	return files
}

// isNonFileLoaderSource reports whether a Loader.Source identifier is one
// of the URL-style or marker-prefix forms whose backing storage cannot be
// observed via fsnotify.
//
// The canonical allowlist is owned by
// [github.com/confiify/confii-go/v2/internal/sourcekind].
func isNonFileLoaderSource(s string) bool {
	return sourcekind.IsNonFileSource(s)
}
