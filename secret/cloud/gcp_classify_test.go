// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build gcp

package cloud

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsGCPAlreadyExists_TypedStatusOnly(t *testing.T) {
	typed := status.Error(codes.AlreadyExists, "secret exists")
	if !isGCPAlreadyExists(typed) {
		t.Error("AlreadyExists status must be accepted")
	}
	if !isGCPAlreadyExists(fmt.Errorf("wrapped: %w", typed)) {
		t.Error("wrapped AlreadyExists status must be accepted")
	}

	for _, err := range []error{
		errors.New("AlreadyExists: unrelated message"),
		errors.New("secret already exists"),
		status.Error(codes.PermissionDenied, "secret already exists"),
	} {
		if isGCPAlreadyExists(err) {
			t.Errorf("non-AlreadyExists error %q must not be accepted", err)
		}
	}
}
