// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import "context"

func (c *Config[T]) implicitOperationContext() (context.Context, context.CancelFunc) {
	if c != nil && c.opts.OperationTimeout > 0 {
		return context.WithTimeout(context.Background(), c.opts.OperationTimeout)
	}
	return context.Background(), func() {}
}
