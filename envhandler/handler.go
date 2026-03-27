// Package envhandler resolves environment-specific configuration by merging
// a "default" section with the active environment section.
package envhandler

import (
	"log/slog"

	"github.com/confiify/confii-go/internal/dictutil"
)

// Handler extracts environment-specific configuration from a merged config.
type Handler struct {
	logger *slog.Logger
}

// New creates a new Handler. If logger is nil, slog.Default() is used.
func New(logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{logger: logger}
}

// Resolve processes the merged config and returns the environment-specific config.
//
// Rules:
//  1. If config has no "default" key and no matching env key → return as-is (flat config).
//  2. If config has "default" → use as base.
//  3. If config has the env key → deep merge on top of default.
//  4. If env not found → warn and fall back to default only.
func (h *Handler) Resolve(config map[string]any, env string) map[string]any {
	defaultSection, hasDefault := config["default"]
	envSection, hasEnv := config[env]

	// No environment structure at all.
	if !hasDefault && !hasEnv {
		return config
	}

	base := make(map[string]any)
	if hasDefault {
		if m, ok := defaultSection.(map[string]any); ok {
			base = m
		}
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
