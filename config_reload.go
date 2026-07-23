package confii

import (
	"context"
	"fmt"
	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/observe"
	"github.com/confiify/confii-go/sourcetrack"
	"github.com/confiify/confii-go/validate"
	"time"
)

// ReloadOption is a functional option that configures the behavior of
// [Config.Reload], such as enabling dry-run mode or overriding validation.
type ReloadOption func(*reloadOpts)

type reloadOpts struct {
	validate    *bool
	dryRun      bool
	incremental bool
}

// WithReloadValidate overrides validate_on_load for this reload. When true,
// the freshly loaded configuration is decoded into T and validated; on
// failure the entire reload is rolled back and a [ErrConfigValidation]
// is returned. When false, validation is skipped regardless of the
// Config's WithValidateOnLoad setting.
//
// G14: validation now runs whether the reload was driven by the
// incremental gate or by an unconditional WithIncremental(false), so a
// validation failure can never leave a "partially observed but not
// re-validated" state in place.
func WithReloadValidate(v bool) ReloadOption {
	return func(o *reloadOpts) { o.validate = &v }
}

// WithDryRun loads, validates, and then rolls back without applying
// changes. The Config's live state is unchanged on a successful dry-run.
//
// G14: dry-run rollback now runs *before* observability emission. A
// dry-run never produces a "reload" metric or event because the new
// state was not committed; if observability needs to record dry-run
// activity, register a listener on a future "dry_run" event (not yet
// emitted).
func WithDryRun(v bool) ReloadOption {
	return func(o *reloadOpts) { o.dryRun = v }
}

// WithIncremental enables per-source change detection. When true (the
// default), unchanged local file layers are reused and only changed files
// are loaded again. Sources without a local fingerprint (HTTP, cloud,
// environment, and custom remote loaders) are refreshed on every call.
// The complete ordered layer set is always re-merged and validated before
// commit, so skipping I/O never skips correctness checks.
func WithIncremental(v bool) ReloadOption {
	return func(o *reloadOpts) { o.incremental = v }
}

// Reload reloads all configurations from their sources.
//
// The reload pipeline is structured as seven strictly-ordered phases
// (G14 — observability ordering contract):
//
//  1. Frozen-state check: refuses with [ErrConfigFrozen] if the Config
//     was frozen.
//  2. Incremental selection: when [WithIncremental] is true (the default),
//     unchanged file layers are reused; untrackable sources are refreshed.
//     If every source is a tracked, unchanged file, Reload returns without
//     metrics or events.
//  3. Snapshot: the full live state (envConfig, mergedConfig, source
//     tracker) is snapshotted so a subsequent rollback restores
//     introspection alongside data (D05 / G14).
//  4. Load: selected loaders run and all cached layers are re-merged. On a Raise-policy
//     error the snapshots are restored, [Metrics.RecordReloadFailed] is
//     called, the "reload_failed" event is emitted, and the original
//     error is returned.
//  5. Validate: when validation is requested (either via Config-level
//     WithValidateOnLoad or per-call WithReloadValidate), the new state
//     is decoded into T and validated. On failure the snapshots are
//     restored and a [*ConfigError] wrapping [ErrConfigValidation] is
//     returned, also accompanied by RecordReloadFailed and a
//     "reload_failed" event.
//  6. Dry-run apply: when WithDryRun(true) is set, the snapshots are
//     restored as-if-validate-then-discard, no commit-time observability
//     fires, and Reload returns nil.
//  7. Commit: only after all earlier phases succeed do we emit the
//     "reload" metric/event, run change callbacks, and emit the
//     "change" event.
//
// Pre-G14, RecordReload and the "reload" event fired *after* c.load
// succeeded but *before* validation/dry-run rollback. That meant a
// validation-failure rollback left observability claiming the new state
// was applied; this is now fixed.
func (c *Config[T]) Reload(ctx context.Context, opts ...ReloadOption) error {
	c.mu.Lock()
	// G13: callbacks must run with c.mu released so a callback that
	// calls Get/Set/Reload/etc. cannot deadlock against the write
	// lock we hold here. We replace the simple `defer c.mu.Unlock()`
	// with a flag so the success path can release the lock manually
	// before invoking notifyChangesUnlocked, while every error/early
	// return continues to release through the deferred fallback.
	unlocked := false
	var failureEventErr error
	var failureEventDuration time.Duration
	var failureEmitter *observe.EventEmitter
	defer func() {
		if !unlocked {
			c.mu.Unlock()
		}
		if failureEventErr != nil && failureEmitter != nil {
			failureEmitter.Emit("reload_failed", failureEventErr, failureEventDuration)
		}
	}()

	if c.frozen {
		return NewFrozenError("Reload")
	}

	ro := reloadOpts{incremental: true}
	for _, o := range opts {
		o(&ro)
	}

	// Phase 2: select only changed trackable sources. Sources that cannot
	// expose a local content fingerprint (HTTP, cloud, environment, custom
	// loaders) are refreshed because skipping them would make remote changes
	// permanently invisible. Unchanged file layers are reused from the cache.
	var selectedSources map[string]bool
	if ro.incremental {
		selectedSources = make(map[string]bool)
		for _, l := range c.loaders {
			source := l.Source()
			if !c.fileTracker.IsTrackable(source) || c.fileTracker.HasChanged(source) {
				selectedSources[source] = true
			}
		}
		for i, dependencies := range c.loaderDependencies {
			for _, dependency := range dependencies {
				if c.fileTracker.HasChanged(dependency) {
					selectedSources[c.loaders[i].Source()] = true
					break
				}
			}
		}
		if len(selectedSources) == 0 {
			return nil
		}
	}

	// Phase 3: snapshot every component c.load mutates so the rollback
	// closure can restore them atomically. fileTracker participates
	// because c.load records mtime+sha256 of each loaded file; without
	// rolling it back, a subsequent incremental Reload would see the
	// recorded-bad hash and short-circuit the change-detection gate.
	oldEnv := copyMap(c.envConfig)
	oldMerged := copyMap(c.mergedConfig)
	trackerSnap := c.sourceTracker.Snapshot()
	fileTrackerSnap := c.fileTracker.Snapshot()
	oldLayers := copyLoaderLayers(c.loaderLayers)
	oldDependencies := copyLoaderDependencies(c.loaderDependencies)
	start := time.Now()

	// rollback restores every snapshot taken in phase 3 and then drives
	// the failure-path observability hooks. It is a closure so that all
	// failure exits (load, validate) share the same restoration logic.
	rollback := func(failureErr error) {
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		c.fileTracker.Restore(fileTrackerSnap)
		c.loaderLayers = oldLayers
		c.loaderDependencies = oldDependencies
		// G14 ordering: failure-path metrics/events fire only after
		// rollback has restored the state, so any observer that probes
		// the Config from inside a "reload_failed" listener sees the
		// pre-reload values, not the doomed post-load ones.
		dur := time.Since(start)
		if c.observer != nil {
			c.observer.RecordReloadFailed(dur)
		}
		failureEventErr = failureErr
		failureEventDuration = dur
		failureEmitter = c.eventEmitter
	}

	// Phase 4: Load.
	//
	// G08: reset the source tracker before c.load runs so a successful
	// reload starts from an empty tracker and re-populates from the new
	// layers. Pre-G08 the tracker was carried across reloads, which (a)
	// inflated override counts on every Reload of the same data because
	// each loader's TrackConfig retraced existing keys, and (b) left
	// stale entries for keys present in v1 but absent in v2 of a config.
	// Resetting via Restore on a zero-value Snapshot clears the live
	// tracker without touching trackerSnap, which the rollback closure
	// above still owns. Failure paths (load/validate) call rollback,
	// which Restores the pre-reload snapshot — preserving the Wave 7
	// G14 / D05 rollback contract.
	c.sourceTracker.Restore(sourcetrack.Snapshot{})
	if err := c.loadSelected(ctx, selectedSources); err != nil {
		rollback(err)
		return err
	}

	// Phase 5: Validate.
	//
	// G14: validation runs unconditionally after selected layers are rebuilt.
	// Pre-G14 the early-return gate at the top
	// of Reload could leave a Config in an unvalidated state when files
	// did change, because the gate path skipped validation. The gate
	// only short-circuits when nothing changed, in which case the
	// previously validated state is still current.
	shouldValidate := c.opts.ValidateOnLoad
	if ro.validate != nil {
		shouldValidate = *ro.validate
	}
	if shouldValidate {
		c.validatedModel = nil
		// G01: when a JSON Schema is configured (inline map or file
		// path), validate the new envConfig against it before the
		// struct decode below. Schema violations roll back via the
		// shared rollback closure to preserve the Wave 7 G14 / D05
		// snapshot contract. Sanitized public message; structured
		// detail on Context["schema_errors"].
		if c.jsonSchema != nil {
			msgs, serr := c.jsonSchema.ValidateDetailed(c.envConfig)
			if serr != nil {
				count := max(1, len(msgs))
				validationErr := &ConfigError{
					Op: "Reload",
					Err: fmt.Errorf(
						"%w: schema validation failed for %d constraint(s)",
						ErrConfigValidation, count,
					),
					Context: map[string]any{
						"schema_errors": msgs,
					},
				}
				rollback(validationErr)
				return validationErr
			}
		}
		if configTypeSupportsStructValidation[T]() {
			if _, err := validate.DecodeAndValidate[T](c.envConfig); err != nil {
				validationErr := NewValidationError([]string{err.Error()}, err)
				rollback(validationErr)
				return validationErr
			}
		}
	}

	// Phase 6: Dry run. Restore snapshots without firing the commit-path
	// observability — a dry-run that completes successfully is *not* a
	// reload from the perspective of metrics or events. The logger
	// records that the dry-run finished so operators have a trace.
	if ro.dryRun {
		c.envConfig = oldEnv
		c.mergedConfig = oldMerged
		c.sourceTracker.Restore(trackerSnap)
		c.fileTracker.Restore(fileTrackerSnap)
		c.loaderLayers = oldLayers
		c.loaderDependencies = oldDependencies
		c.logger.Info("dry-run reload completed, changes not applied")
		return nil
	}

	// Phase 7: Commit. Only now is it safe to claim that a reload
	// happened — the new state is live and validated, callbacks below
	// can rely on it, and observability can record both reload and
	// change in the same critical section.
	c.validatedModel = nil
	duration := time.Since(start)
	if c.observer != nil {
		c.observer.RecordReload(duration)
	}

	// G13: snapshot everything callbacks need WHILE the write lock is
	// held, then release the lock BEFORE iterating callbacks. This
	// guarantees a callback that calls back into the Config (Get/Set/
	// Reload/Has/etc.) cannot deadlock on c.mu. The "newEnv" snapshot
	// is also used for the post-callback "change" event so that a
	// concurrent Set/Reload landing between unlock and Emit does not
	// lie about the payload that was observed.
	callbacks := c.snapshotChangeCallbacks()
	oldFlat := dictutil.Flatten(oldEnv)
	newFlat := dictutil.Flatten(c.envConfig)
	newEnv := copyMap(c.envConfig)
	observer := c.observer
	emitter := c.eventEmitter

	c.mu.Unlock()
	unlocked = true

	if emitter != nil {
		emitter.Emit("reload", newEnv, duration)
	}
	c.notifyChangesUnlocked(callbacks, oldFlat, newFlat)

	if observer != nil {
		observer.RecordChange()
	}
	if emitter != nil {
		emitter.Emit("change", oldEnv, newEnv)
	}

	return nil
}
