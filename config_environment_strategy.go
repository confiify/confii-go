// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"
	"sort"
	"strings"
)

// EnvironmentStrategy controls how environment-aware configuration sources
// may be combined. The zero/default value uses section-based behavior unless
// an environment_files source is declared, in which case
// Confii infers EnvironmentStrategyNamedFiles.
type EnvironmentStrategy string

const (
	// EnvironmentStrategyAuto uses sectioned configuration unless a declared
	// environment_files source makes the named-files model unambiguous.
	EnvironmentStrategyAuto EnvironmentStrategy = "auto"
	// EnvironmentStrategySectioned selects an environment from top-level
	// default and environment mappings in a loaded document.
	EnvironmentStrategySectioned EnvironmentStrategy = "sectioned"
	// EnvironmentStrategyNamedFiles loads a shared default file followed by the
	// file associated with the active environment.
	EnvironmentStrategyNamedFiles EnvironmentStrategy = "named_files"
	// EnvironmentStrategyHybrid explicitly permits sectioned and named-file
	// sources in one project and applies EnvironmentConflictPolicy to overlaps.
	EnvironmentStrategyHybrid EnvironmentStrategy = "hybrid"
)

// EnvironmentConflictPolicy controls conflicts between a section-based
// environment source and a named environment file in explicit hybrid mode.
type EnvironmentConflictPolicy string

const (
	// EnvironmentConflictError rejects a hybrid source plan when both models
	// write the same key.
	EnvironmentConflictError EnvironmentConflictPolicy = "error"
	// EnvironmentConflictWarn records and logs hybrid conflicts while retaining
	// the value selected by source precedence.
	EnvironmentConflictWarn EnvironmentConflictPolicy = "warn"
	// EnvironmentConflictLastWins resolves hybrid conflicts solely by ordered
	// source precedence without logging a warning.
	EnvironmentConflictLastWins EnvironmentConflictPolicy = "last_wins"
)

// SourcePlanLayer describes one resolved loader in precedence order.
type SourcePlanLayer struct {
	// Order is the zero-based precedence position; larger values have higher precedence.
	Order int `json:"order"`
	// Source is the loader's stable source identifier.
	Source string `json:"source"`
	// LoaderType is the normalized concrete loader type.
	LoaderType string `json:"loader_type"`
	// Role describes the layer's environment-model role.
	Role string `json:"role"`
	// Keys contains the sorted dot-separated leaf paths contributed by the layer.
	Keys []string `json:"keys,omitempty"`
}

// EnvironmentSourceConflict describes a key written by both environment
// models in explicit hybrid mode. Sources are ordered from lowest to highest
// precedence; LastWriter is the final environment-model writer for that key.
type EnvironmentSourceConflict struct {
	// Key is the conflicting dot-separated key path.
	Key string `json:"key"`
	// Sources lists writers from lowest to highest precedence.
	Sources []string `json:"sources"`
	// LastWriter is the source whose value was published.
	LastWriter string `json:"last_writer"`
}

// SourcePlan is an immutable snapshot of the environment model, ordered
// sources, and mixed-model conflicts observed during the last successful load.
type SourcePlan struct {
	// Environment is the selected environment, or empty when none was selected.
	Environment string `json:"environment"`
	// Strategy is the effective environment interpretation strategy.
	Strategy EnvironmentStrategy `json:"strategy"`
	// ConflictPolicy is the policy applied to mixed-model overlaps.
	ConflictPolicy EnvironmentConflictPolicy `json:"conflict_policy"`
	// Layers is ordered from lowest to highest precedence.
	Layers []SourcePlanLayer `json:"layers"`
	// Conflicts contains mixed-model overlaps detected during the load.
	Conflicts []EnvironmentSourceConflict `json:"conflicts,omitempty"`
}

func parseEnvironmentStrategy(value string) (EnvironmentStrategy, error) {
	strategy := EnvironmentStrategy(strings.ToLower(strings.TrimSpace(value)))
	switch strategy {
	case EnvironmentStrategyAuto, EnvironmentStrategySectioned,
		EnvironmentStrategyNamedFiles, EnvironmentStrategyHybrid:
		return strategy, nil
	default:
		return "", environmentStrategyError(
			fmt.Sprintf("invalid environment_strategy %q (valid values: %q, %q, %q, %q)",
				value,
				EnvironmentStrategyAuto,
				EnvironmentStrategySectioned,
				EnvironmentStrategyNamedFiles,
				EnvironmentStrategyHybrid,
			),
		)
	}
}

func parseEnvironmentConflictPolicy(value string) (EnvironmentConflictPolicy, error) {
	policy := EnvironmentConflictPolicy(strings.ToLower(strings.TrimSpace(value)))
	switch policy {
	case EnvironmentConflictError, EnvironmentConflictWarn, EnvironmentConflictLastWins:
		return policy, nil
	default:
		return "", environmentStrategyError(
			fmt.Sprintf("invalid environment_conflict_policy %q (valid values: %q, %q, %q)",
				value,
				EnvironmentConflictError,
				EnvironmentConflictWarn,
				EnvironmentConflictLastWins,
			),
		)
	}
}

func resolveEnvironmentStrategy(opts *options) error {
	strategy, err := parseEnvironmentStrategy(string(opts.EnvironmentStrategy))
	if err != nil {
		return err
	}
	opts.EnvironmentStrategy = strategy
	policy, err := parseEnvironmentConflictPolicy(string(opts.EnvironmentConflictPolicy))
	if err != nil {
		return err
	}
	opts.EnvironmentConflictPolicy = policy

	hasEnvironmentFiles := false
	for _, src := range opts.selfConfigSources {
		rawType, _ := src["type"].(string)
		t := strings.ToLower(strings.TrimSpace(rawType))
		if t == "environment_files" {
			hasEnvironmentFiles = true
			break
		}
	}

	if opts.EnvironmentStrategy == EnvironmentStrategyAuto && hasEnvironmentFiles {
		opts.EnvironmentStrategy = EnvironmentStrategyNamedFiles
	}

	switch opts.EnvironmentStrategy {
	case EnvironmentStrategySectioned:
		if hasEnvironmentFiles {
			return environmentStrategyError("environment_strategy \"sectioned\" cannot be combined with an environment_files source")
		}
	case EnvironmentStrategyNamedFiles:
		if !hasEnvironmentFiles && !opts.isSet("loaders") {
			return environmentStrategyError("environment_strategy \"named_files\" requires an environment_files source")
		}
	case EnvironmentStrategyHybrid:
		if !hasEnvironmentFiles && !opts.isSet("loaders") {
			return environmentStrategyError("environment_strategy \"hybrid\" requires an environment_files source")
		}
		if !opts.environmentConflictPolicyConfigured {
			return environmentStrategyError("environment_strategy \"hybrid\" requires an explicit environment_conflict_policy")
		}
	}

	return nil
}

type environmentSourceWriter struct {
	family string
	source string
}

func mixedEnvironmentConflicts(writers map[string][]environmentSourceWriter) []EnvironmentSourceConflict {
	conflicts := make([]EnvironmentSourceConflict, 0)
	for key, keyWriters := range writers {
		families := make(map[string]bool, 2)
		sources := make([]string, 0, len(keyWriters))
		seenSources := make(map[string]bool, len(keyWriters))
		for _, writer := range keyWriters {
			families[writer.family] = true
			if !seenSources[writer.source] {
				seenSources[writer.source] = true
				sources = append(sources, writer.source)
			}
		}
		if len(families) < 2 {
			continue
		}
		conflicts = append(conflicts, EnvironmentSourceConflict{
			Key:        key,
			Sources:    sources,
			LastWriter: keyWriters[len(keyWriters)-1].source,
		})
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Key < conflicts[j].Key })
	return conflicts
}

func environmentConflictSummary(conflicts []EnvironmentSourceConflict) string {
	parts := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		parts = append(parts, fmt.Sprintf("%s (%s; last writer: %s)",
			conflict.Key,
			strings.Join(conflict.Sources, " -> "),
			conflict.LastWriter,
		))
	}
	return strings.Join(parts, "; ")
}

func environmentStrategyError(message string) error {
	return &ConfigError{
		Op:   "ApplySelfConfig",
		Code: ConfigErrorCodeLoad,
		Err:  fmt.Errorf("environment strategy: %s", message),
	}
}

func cloneSourcePlan(plan SourcePlan) SourcePlan {
	cloned := plan
	cloned.Layers = make([]SourcePlanLayer, len(plan.Layers))
	for i, layer := range plan.Layers {
		cloned.Layers[i] = layer
		cloned.Layers[i].Keys = append([]string(nil), layer.Keys...)
	}
	cloned.Conflicts = make([]EnvironmentSourceConflict, len(plan.Conflicts))
	for i, conflict := range plan.Conflicts {
		cloned.Conflicts[i] = conflict
		cloned.Conflicts[i].Sources = append([]string(nil), conflict.Sources...)
	}
	return cloned
}
