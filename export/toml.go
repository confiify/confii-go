package export

import "github.com/BurntSushi/toml"

// TOMLExporter exports configuration as TOML.
type TOMLExporter struct{}

// Export serializes the configuration data as TOML.
func (e *TOMLExporter) Export(data map[string]any) ([]byte, error) {
	var buf []byte
	// toml.Marshal doesn't exist, use Encoder.
	var b = new(tomlBuffer)
	enc := toml.NewEncoder(b)
	if err := enc.Encode(data); err != nil {
		return nil, err
	}
	buf = b.Bytes()
	return buf, nil
}

// Format returns "toml".
func (e *TOMLExporter) Format() string { return "toml" }

// tomlBuffer implements io.Writer for toml.Encoder.
type tomlBuffer struct {
	data []byte
}

func (b *tomlBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *tomlBuffer) Bytes() []byte {
	return b.data
}
