package confii

import "github.com/confiify/confii-go/merge"

type (
	// MergeStrategy identifies the algorithm used when two configuration maps
	// are combined during a load or reload operation.
	MergeStrategy = merge.Strategy

	// Merger is a function that combines a base configuration map with an
	// overlay map, producing a merged result according to a [MergeStrategy].
	Merger = merge.Merger
)

const (
	// StrategyReplace discards the base map entirely and keeps only the overlay.
	StrategyReplace = merge.Replace

	// StrategyMerge recursively deep-merges the overlay into the base map,
	// preserving base keys that do not appear in the overlay.
	StrategyMerge = merge.DeepMergeStrategy

	// StrategyAppend appends overlay slice elements after the base slice elements.
	StrategyAppend = merge.Append

	// StrategyPrepend inserts overlay slice elements before the base slice elements.
	StrategyPrepend = merge.Prepend

	// StrategyIntersection keeps only keys that exist in both the base and the overlay.
	StrategyIntersection = merge.Intersection

	// StrategyUnion keeps all keys from both the base and the overlay,
	// with overlay values taking precedence on conflicts.
	StrategyUnion = merge.Union
)
