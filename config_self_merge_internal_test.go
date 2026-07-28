// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSelfConfigMergeStrategyAliases(t *testing.T) {
	for input, want := range map[string]MergeStrategy{
		"replace":      StrategyReplace,
		"merge":        StrategyMerge,
		"deep_merge":   StrategyMerge,
		"deep-merge":   StrategyMerge,
		"append":       StrategyAppend,
		"prepend":      StrategyPrepend,
		"intersection": StrategyIntersection,
		"union":        StrategyUnion,
	} {
		got, err := parseSelfConfigMergeStrategy(input)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}
