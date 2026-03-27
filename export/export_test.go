package export

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testData = map[string]any{
	"database": map[string]any{
		"host": "localhost",
		"port": int64(5432),
	},
	"debug": true,
}

func TestJSONExporter(t *testing.T) {
	e := &JSONExporter{}
	assert.Equal(t, "json", e.Format())

	data, err := e.Export(testData)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"host": "localhost"`)
}

func TestYAMLExporter(t *testing.T) {
	e := &YAMLExporter{}
	assert.Equal(t, "yaml", e.Format())

	data, err := e.Export(testData)
	require.NoError(t, err)
	assert.Contains(t, string(data), "host: localhost")
}

func TestTOMLExporter(t *testing.T) {
	e := &TOMLExporter{}
	assert.Equal(t, "toml", e.Format())

	data, err := e.Export(testData)
	require.NoError(t, err)
	assert.Contains(t, string(data), "host")
}

func TestTOMLExporter_UnencodableValue(t *testing.T) {
	e := &TOMLExporter{}

	// A channel cannot be encoded by TOML.
	ch := make(chan int)
	badData := map[string]any{
		"channel": ch,
	}

	_, err := e.Export(badData)
	assert.Error(t, err)
}
