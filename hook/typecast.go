// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"

	"github.com/confiify/confii-go/v2/internal/typecoerce"
)

// NewTypeCastHook returns a hook that converts string values to their
// most appropriate Go type (bool, int, float64).
func NewTypeCastHook() Func {
	return func(_ context.Context, _ string, value any) (any, error) {
		s, ok := value.(string)
		if !ok {
			return value, nil
		}
		return typecoerce.ParseScalar(s, false), nil
	}
}
