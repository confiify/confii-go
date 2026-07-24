package confii

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/confiify/confii-go/merge"
)

// load loads and merges all configurations with source tracking and composition.
//
// Errors from individual loaders and from the composition pass are routed
// through c.opts.OnError so that the three [ErrorPolicy] values behave
// distinctly (G07):
//
//   - [ErrorPolicyRaise]: the first error is returned immediately; New /
//     Reload propagate it to the caller.
//   - [ErrorPolicyWarn]: the error is logged at warn level via c.logger
//     and the source is skipped (loaders) or its raw data is used
//     (composer).
//   - [ErrorPolicyIgnore]: the error is silently swallowed with no log
//     record at all — distinct from Warn, which always logs.
func (c *Config[T]) load(ctx context.Context) error {
	return c.loadSelected(ctx, nil)
}

// loadSelected refreshes selected loader layers and then deterministically
// rebuilds merged/env config from the complete layer cache. A nil selector
// performs a full load. The caller holds c.mu for Reload; New invokes this
// before the Config is published.
func (c *Config[T]) loadSelected(ctx context.Context, selected map[string]bool) error {
	var configs []map[string]any
	var resolvedConfigs []map[string]any
	usesEnvironmentFiles := false
	if len(c.loaderLayers) != len(c.loaders) {
		c.loaderLayers = make([]map[string]any, len(c.loaders))
		c.loaderDependencies = make([][]string, len(c.loaders))
		selected = nil
	}

	for i, l := range c.loaders {
		selectedFile, isEnvironmentFile := l.(interface{ selectedEnvironmentFile() bool })
		isEnvironmentFile = isEnvironmentFile && selectedFile.selectedEnvironmentFile()
		usesEnvironmentFiles = usesEnvironmentFiles || isEnvironmentFile
		shouldLoad := selected == nil || selected[l.Source()]
		if !shouldLoad {
			if c.loaderLayers[i] != nil {
				layer := copyMap(c.loaderLayers[i])
				resolvedLayer := c.envHandler.Resolve(layer, c.env)
				if isEnvironmentFile {
					resolvedLayer = layer
				}
				c.sourceTracker.TrackConfig(resolvedLayer, l.Source(), loaderTypeName(l), c.env, "")
				configs = append(configs, layer)
				resolvedConfigs = append(resolvedConfigs, resolvedLayer)
			}
			continue
		}
		data, err := l.Load(ctx)
		if err != nil {
			switch c.opts.OnError {
			case ErrorPolicyRaise:
				return err
			case ErrorPolicyWarn:
				c.logger.Warn(
					"loader error",
					slog.String("source", l.Source()),
					slog.String("error", err.Error()),
				)
			case ErrorPolicyIgnore:
				// Silent: distinct from Warn, do not emit a log record.
			default:
				// Unknown policy values are treated as Raise so that
				// misconfiguration surfaces loudly rather than degrading
				// silently to Warn-or-Ignore behavior.
				return err
			}
			c.loaderLayers[i] = nil
			c.loaderDependencies[i] = nil
			continue
		}
		if data == nil {
			c.loaderLayers[i] = nil
			c.loaderDependencies[i] = nil
			continue
		}

		// Process composition directives (_include, _defaults). Errors
		// here used to be swallowed and the raw (un-composed) data was
		// loaded regardless of policy. Post-G07, composition errors flow
		// through the same OnError dispatch as loader errors.
		composed, dependencies, err := c.composer.ComposeWithDependencies(data, l.Source())
		if err != nil {
			switch c.opts.OnError {
			case ErrorPolicyRaise:
				return err
			case ErrorPolicyWarn:
				c.logger.Warn(
					"composition error",
					slog.String("source", l.Source()),
					slog.String("error", err.Error()),
				)
				composed = data
				dependencies = nil
			case ErrorPolicyIgnore:
				composed = data
				dependencies = nil
			default:
				return err
			}
		}

		// G08: Track each loader's RESOLVED contribution against the loader
		// source so introspection reports resolved key paths
		// (e.g. "database.host" not "production.database.host") AND attributes
		// every key to the file/loader it actually came from. Pre-G08 this
		// path tracked the raw composed data and then issued a second
		// TrackConfig pass with the synthetic source string "(resolved)" that
		// overwrote the loader source for every env-resolved key. Tracking
		// the resolved-per-loader output here mirrors the pattern already
		// used by Config.Extend (see config.go:1325-1329) and is Option A
		// from the G08 closure plan: skip the post-resolve retrack entirely
		// because the loader source is already the right answer.
		loaderType := loaderTypeName(l)
		resolvedLayer := c.envHandler.Resolve(composed, c.env)
		if isEnvironmentFile {
			// This file was already selected for the active environment;
			// its contents are flat application configuration, even if a
			// legitimate key happens to equal the environment name.
			resolvedLayer = composed
		}
		c.sourceTracker.TrackConfig(resolvedLayer, l.Source(), loaderType, c.env, "")

		// Track file for incremental reload.
		_ = c.fileTracker.Track(l.Source())
		for _, dependency := range dependencies {
			_ = c.fileTracker.Track(dependency)
		}

		c.loaderLayers[i] = copyMap(composed)
		c.loaderDependencies[i] = append([]string(nil), dependencies...)
		configs = append(configs, composed)
		resolvedConfigs = append(resolvedConfigs, resolvedLayer)
	}

	c.mergedConfig = merge.MergeAll(c.merger, configs...)
	if usesEnvironmentFiles {
		// Resolve every other source independently, then merge all resolved
		// contributions in loader order. This lets an environment_files
		// source coexist with section-based files, environment variables,
		// and remote loaders without the final section resolver discarding
		// their flat keys.
		c.envConfig = merge.MergeAll(c.merger, resolvedConfigs...)
	} else {
		c.envConfig = c.envHandler.Resolve(c.mergedConfig, c.env)
	}

	return nil
}

// loaderTypeName returns the underlying type name of a Loader implementation,
// suitable for use in source tracking and debug output.
//
// The Loader interface accepts both pointer and value receiver implementations,
// so we cannot assume reflect.TypeOf(l) returns a pointer kind. Calling
// reflect.Type.Elem on a non-pointer (and non-array/chan/map/slice) type panics
// with "reflect: Elem of invalid type", which previously broke any custom
// Loader registered as a value type. reflect.Indirect on the value normalizes
// pointer and value loaders to the same underlying type before we read the
// name. If the type is anonymous (Name() == ""), we fall back to the
// reflect.Type string representation so the loader still surfaces something
// meaningful in debug output.
func loaderTypeName(l Loader) string {
	t := reflect.TypeOf(l)
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if name := t.Name(); name != "" {
		return name
	}
	return reflect.TypeOf(l).String()
}
