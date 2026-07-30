// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSelfConfigCanonicalMergeStrategies(t *testing.T) {
	for input, want := range map[string]MergeStrategy{
		"replace":       StrategyReplace,
		"shallow_merge": StrategyShallowMerge,
		"deep_merge":    StrategyMerge,
		"append":        StrategyAppend,
		"prepend":       StrategyPrepend,
		"intersection":  StrategyIntersection,
		"union":         StrategyUnion,
	} {
		got, err := parseSelfConfigMergeStrategy(input)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	for _, removed := range []string{"merge", "deep-merge"} {
		_, err := parseSelfConfigMergeStrategy(removed)
		require.Error(t, err)
	}
}
