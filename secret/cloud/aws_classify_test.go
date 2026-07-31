// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

package cloud

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

// Not-found is the only classification that lets multi-store fallback
// continue, so it must match the SDK's typed condition and nothing else.
func TestIsAWSNotFound_TypedErrorOnly(t *testing.T) {
	typed := fmt.Errorf("wrapped: %w", &types.ResourceNotFoundException{})
	if !isAWSNotFound(typed) {
		t.Error("typed ResourceNotFoundException must classify as not-found")
	}

	for _, err := range []error{
		errors.New("dial tcp: host not found"),
		errors.New("AccessDeniedException: not authorized"),
		errors.New("endpoint not found"),
	} {
		if isAWSNotFound(err) {
			t.Errorf("message-only error %q must not classify as not-found", err)
		}
	}
}
