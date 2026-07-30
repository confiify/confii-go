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

func TestSetNested(t *testing.T) {
	data := map[string]any{}
	if err := SetNested(data, "database.host", "localhost"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"database": map[string]any{"host": "localhost"}}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("SetNested result = %#v, want %#v", data, want)
	}

	if err := SetNested(data, "database.port", 5432); err != nil {
		t.Fatal(err)
	}
	if err := SetNested(data, "database.host.value", "invalid"); err == nil {
		t.Fatal("SetNested accepted a scalar intermediate")
	}
	if err := SetNested(data, "", "invalid"); err == nil {
		t.Fatal("SetNested accepted an empty path")
	}
}
