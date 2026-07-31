// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package envhandler resolves environment-specific configuration by merging
// a "default" section with the active environment section.
//
// # Environment-mode detection
//
// The handler treats a config as environment-aware only when the structure
// gives an explicit signal. A scalar `default` key remains an ordinary key in
// flat configuration such as:
//
//	default: en-US     # locale default, not a config section
//	bar: 2
//	baz: 3
//
// Environment mode requires one of the following signals before the handler
// will consume "default" as a base section and discard everything that is
// not the active environment:
//
//  1. The active environment name is present as a top-level sibling key.
//     (e.g. resolving env "production" against a config that contains a
//     "production:" key — regardless of that value's type.)
//  2. The "default" key is a map AND at least one other top-level sibling
//     is also a map (i.e. a recognizable environment section). This supports
//     selection of an environment not yet present when other environment
//     sections establish the file's structure.
//
// If neither signal is present, the handler returns the config unchanged.
package envhandler

import (
	"log/slog"
	"os"

	"github.com/confiify/confii-go/v2/internal/dictutil"
)

// ResolveEnv decides which environment name should win when both an explicit
// programmatic value and an OS-variable hook may be in play.
//
// The function encodes the documented precedence:
//
//  1. Explicit programmatic env (e.g. [confii.WithEnv]) — always wins when
//     explicitFlag is true, regardless of explicitEnv's value (an explicit
//     empty string is still a deliberate choice and must be honored).
//  2. OS variable named by osVarName (e.g. [confii.WithEnvSwitcher]) — used
//     only when explicitFlag is false, and only when osVarName is non-empty
//     AND os.Getenv(osVarName) is non-empty.
//  3. Fallback — the function returns fallback (typically the empty
//     string or a previously-resolved self-config default) when neither
//     of the above produces a value.
//
// ResolveEnv centralizes this precedence for construction and reload paths.
func ResolveEnv(explicitEnv string, explicitFlag bool, osVarName, fallback string) string {
	if explicitFlag {
		return explicitEnv
	}
	if osVarName != "" {
		if v := os.Getenv(osVarName); v != "" {
			return v
		}
	}
	return fallback
}

// Handler extracts environment-specific configuration from a merged config.
type Handler struct {
	logger *slog.Logger
}

// New creates a Handler. A nil logger uses [slog.Default].
func New(logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{logger: logger}
}

// Resolve processes the merged config and returns the environment-specific config.
//
// Resolution rules (see package doc for the rationale):
//
//  1. Environment mode is asserted only when the config carries an explicit
//     env-section signal: either the named env exists as a top-level sibling,
//     or "default" is a map and at least one other top-level sibling is a map.
//  2. In environment mode:
//     a. If "default" is a map, it is used as the base.
//     b. If the named env exists as a map, it is deep-merged on top of base.
//     c. If the named env exists but is not a map, base is returned.
//     d. If the named env is missing entirely, the handler warns and returns
//     base (the default section, possibly empty).
//  3. Outside environment mode the config is passed through verbatim. In
//     particular, a lone "default" key has no special meaning in flat or
//     auto mode; callers that want section semantics must select the
//     sectioned strategy.
func (h *Handler) Resolve(config map[string]any, env string) map[string]any {
	defaultSection := config["default"]
	envSection, hasEnv := config[env]

	// Decide whether the config is in environment mode.
	defaultMap, defaultIsMap := defaultSection.(map[string]any)
	envMode := h.IsEnvironmentAware(config, env)

	if !envMode {
		// Flat-config passthrough: return verbatim so legitimate top-level
		// keys are not silently dropped.
		return config
	}

	// In environment mode: build base from "default" (if it is a map).
	base := make(map[string]any)
	if defaultIsMap {
		base = defaultMap
	}

	if !hasEnv {
		if env != "" {
			available := h.availableEnvs(config)
			h.logger.Warn("environment not found in config, using defaults",
				slog.String("env", env),
				slog.Any("available", available),
			)
		}
		return base
	}

	envMap, ok := envSection.(map[string]any)
	if !ok {
		return base
	}

	return dictutil.DeepMerge(base, envMap)
}

// IsEnvironmentAware reports whether config will be interpreted as a set of
// environment sections by Resolve. It exposes Resolve's exact detection rule
// so higher-level source planning can reject accidental mixing without
// maintaining a second, potentially divergent heuristic.
func (h *Handler) IsEnvironmentAware(config map[string]any, env string) bool {
	defaultSection, hasDefault := config["default"]
	_, defaultIsMap := defaultSection.(map[string]any)
	return h.isEnvMode(config, env, hasDefault, defaultIsMap)
}

// isEnvMode reports whether the merged config should be treated as
// environment-aware. See the package documentation for the signal rules.
func (h *Handler) isEnvMode(config map[string]any, env string, hasDefault, defaultIsMap bool) bool {
	// Signal 1: the named env is present as a top-level sibling.
	if env != "" {
		if _, ok := config[env]; ok {
			return true
		}
	}
	// Signal 2: "default" is a map AND another top-level sibling is also a map.
	if hasDefault && defaultIsMap {
		for k, v := range config {
			if k == "default" {
				continue
			}
			if _, ok := v.(map[string]any); ok {
				return true
			}
		}
	}
	return false
}

// availableEnvs returns top-level keys that look like environment sections
// (i.e., their values are maps), excluding "default".
func (h *Handler) availableEnvs(config map[string]any) []string {
	var envs []string
	for k, v := range config {
		if k == "default" {
			continue
		}
		if _, ok := v.(map[string]any); ok {
			envs = append(envs, k)
		}
	}
	return envs
}
