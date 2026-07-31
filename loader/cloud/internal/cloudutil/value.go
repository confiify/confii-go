// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package cloudutil contains implementation details shared by cloud loaders.
package cloudutil

import (
	"strconv"
	"strings"
)

// ParseScalar converts an SSM string to a conservative scalar value.
func ParseScalar(value string) any {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "true":
		return true
	case "false":
		return false
	}
	if i, err := strconv.ParseInt(value, 10, strconv.IntSize); err == nil && strconv.FormatInt(i, 10) == value {
		return int(i)
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil && strings.ContainsAny(value, ".eE") {
		return f
	}
	return value
}
