// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package diff

// DriftDetector compares actual configuration against one intended baseline.
// It is safe for concurrent reads when callers do not mutate the baseline map.
type DriftDetector struct {
	intended map[string]any
}

// NewDriftDetector creates a detector for intended. The map is retained rather
// than copied and must not be mutated while the detector is in use.
func NewDriftDetector(intended map[string]any) *DriftDetector {
	return &DriftDetector{intended: intended}
}

// DetectDrift compares the intended baseline with actual using [Diff]. Added
// means present only in actual, Removed means present only in the baseline,
// and Modified means present in both with unequal values.
func (d *DriftDetector) DetectDrift(actual map[string]any) []ConfigDiff {
	return Diff(d.intended, actual)
}

// HasDrift reports whether DetectDrift would return at least one top-level
// difference.
func (d *DriftDetector) HasDrift(actual map[string]any) bool {
	return len(d.DetectDrift(actual)) > 0
}
