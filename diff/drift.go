package diff

// DriftDetector compares actual configuration against an intended baseline.
type DriftDetector struct {
	intended map[string]any
}

// NewDriftDetector creates a drift detector with the intended configuration.
func NewDriftDetector(intended map[string]any) *DriftDetector {
	return &DriftDetector{intended: intended}
}

// DetectDrift returns diffs between the intended config and the actual config.
func (d *DriftDetector) DetectDrift(actual map[string]any) []ConfigDiff {
	return Diff(d.intended, actual)
}

// HasDrift returns true if there is any drift from intended to actual.
func (d *DriftDetector) HasDrift(actual map[string]any) bool {
	return len(d.DetectDrift(actual)) > 0
}
