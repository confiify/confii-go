package confii

// Exporter serializes configuration to a specific format.
type Exporter interface {
	// Export serializes the configuration data.
	Export(data map[string]any) ([]byte, error)
	// Format returns the format name (e.g., "json", "yaml", "toml").
	Format() string
}
