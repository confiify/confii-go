package formatparse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromExtension(t *testing.T) {
	tests := []struct {
		filename string
		want     Format
	}{
		{"config.yaml", FormatYAML},
		{"config.yml", FormatYAML},
		{"config.json", FormatJSON},
		{"config.toml", FormatTOML},
		{"config.ini", FormatINI},
		{"config.cfg", FormatINI},
		{"config.env", FormatEnvFile},
		{"config.txt", FormatUnknown},
		{"noext", FormatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			assert.Equal(t, tt.want, FromExtension(tt.filename))
		})
	}
}

func TestFromContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want Format
	}{
		{"application/json", FormatJSON},
		{"application/x-yaml", FormatYAML},
		{"application/toml", FormatTOML},
		{"text/plain", FormatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			assert.Equal(t, tt.want, FromContentType(tt.ct))
		})
	}
}
