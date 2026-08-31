// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build vault

package cloud

import (
	"fmt"
	"math"
	"strconv"
)

// Typed readers for declarative provider settings.
//
// These live behind the vault tag because the Vault provider is the only one
// that declares boolean and integer settings. Kept alongside the untagged
// string reader they were dead code in every build that excluded vault, which
// the per-tag lint gate reports and which no single-tag build would have
// noticed while the linter ran only in the repository root.

func selfBool(cfg map[string]any, key string, fallback bool) (bool, error) {
	v, ok := cfg[key]
	if !ok {
		return fallback, nil
	}
	switch value := v.(type) {
	case bool:
		return value, nil
	case string:
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed, nil
		}
	}
	return false, fmt.Errorf("%s must be a boolean", key)
}

func selfInt(cfg map[string]any, key string, fallback int) (int, error) {
	v, ok := cfg[key]
	if !ok {
		return fallback, nil
	}
	switch value := v.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		// int(value) silently truncates, so 1.5 became 1 and a value beyond
		// int range wrapped. A configuration that says 1.5 does not mean 1.
		if value != math.Trunc(value) {
			return 0, fmt.Errorf("%s must be a whole number", key)
		}
		if value > math.MaxInt || value < math.MinInt {
			return 0, fmt.Errorf("%s is out of range", key)
		}
		return int(value), nil
	case string:
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("%s must be an integer", key)
}
