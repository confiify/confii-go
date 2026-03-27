package confii

import "github.com/confiify/confii-go/merge"

// Re-export merge types for convenience.
type (
	// MergeStrategy defines how two configuration maps are combined.
	MergeStrategy = merge.Strategy
	// Merger combines two configuration maps.
	Merger = merge.Merger
)

// Merge strategy constants re-exported for convenience.
const (
	StrategyReplace      = merge.Replace
	StrategyMerge        = merge.DeepMergeStrategy
	StrategyAppend       = merge.Append
	StrategyPrepend      = merge.Prepend
	StrategyIntersection = merge.Intersection
	StrategyUnion        = merge.Union
)
