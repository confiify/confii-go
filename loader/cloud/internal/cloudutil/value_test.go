// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cloudutil

import (
	"reflect"
	"testing"
)

func TestParseScalar(t *testing.T) {
	tests := map[string]any{
		"true":  true,
		"FALSE": false,
		"42":    42,
		"01":    "01",
		"3.5":   3.5,
		"1e3":   1000.0,
		"value": "value",
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			if got := ParseScalar(input); !reflect.DeepEqual(got, want) {
				t.Fatalf("ParseScalar(%q) = %#v, want %#v", input, got, want)
			}
		})
	}
}
