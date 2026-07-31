// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
)

// operationCancellation returns the authoritative cancellation failure for an
// operation. Cancellation is control flow, not a recoverable source error, so
// callers must check this before applying warn/ignore policies or fallbacks.
func operationCancellation(ctx context.Context, err error) error {
	if ctx != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
