// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTypeCastHook(t *testing.T) {
	h := NewTypeCastHook()

	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"string true", "true", true},
		{"string false", "false", false},
		{"string int", "42", 42},
		{"string float", "3.14", 3.14},
		{"plain string", "hello", "hello"},
		{"non-string passthrough", 42, 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h(context.Background(), "key", tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
