// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package hook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvExpanderHook(t *testing.T) {
	t.Setenv("DB_HOST", "prod-db")
	t.Setenv("DB_PORT", "5432")

	h := NewEnvExpanderHook()

	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"simple expansion", "${DB_HOST}", "prod-db"},
		{"named expansion", "${env:DB_HOST}", "prod-db"},
		{"multiple expansions", "${DB_HOST}:${DB_PORT}", "prod-db:5432"},
		{"mixed expansion", "${env:DB_HOST}:${DB_PORT}", "prod-db:5432"},
		{"no match left unchanged", "${NONEXISTENT}", "${NONEXISTENT}"},
		{"named no match left unchanged", "${env:NONEXISTENT}", "${env:NONEXISTENT}"},
		{"no placeholder", "plain_value", "plain_value"},
		{"non-string passthrough", 42, 42},
		{"partial match", "host=${DB_HOST}", "host=prod-db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h(context.Background(), "key", tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
