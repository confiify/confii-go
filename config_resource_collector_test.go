// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingCloser struct{ closed int }

func (c *countingCloser) Close() error { c.closed++; return nil }

// The self-config resource collector owns lazily created secret providers. Its
// contract guards against both leaks and double-closes: a provider that
// finishes initializing after the Config has closed must be rejected so its
// caller closes it immediately, and closeAndSnapshot must be idempotent.
func TestSelfConfigResourceCollector_RejectsAfterCloseExactlyOnce(t *testing.T) {
	collector := newSelfConfigResourceCollector()

	early := &countingCloser{}
	require.True(t, collector.add(early), "add before close must succeed")

	snapshot := collector.closeAndSnapshot()
	require.Len(t, snapshot, 1, "closeAndSnapshot must return each collected resource exactly once")
	assert.Same(t, early, snapshot[0])

	assert.Nil(t, collector.closeAndSnapshot(),
		"closeAndSnapshot must be idempotent so a resource is never handed out twice for closing")

	late := &countingCloser{}
	assert.False(t, collector.add(late),
		"a provider that initializes after close must be rejected so its caller closes it, not leaked")
}
