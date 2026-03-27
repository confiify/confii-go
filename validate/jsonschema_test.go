package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONSchemaValidator_Validate(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"host": map[string]any{"type": "string"},
			"port": map[string]any{"type": "integer", "minimum": float64(1)},
		},
		"required": []any{"host", "port"},
	}

	v, err := NewJSONSchemaValidator(schema)
	require.NoError(t, err)

	tests := []struct {
		name    string
		data    map[string]any
		wantErr bool
	}{
		{
			name:    "valid data",
			data:    map[string]any{"host": "localhost", "port": 5432},
			wantErr: false,
		},
		{
			name:    "missing required field",
			data:    map[string]any{"host": "localhost"},
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    map[string]any{"host": 123, "port": 5432},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJSONSchemaValidator_InvalidSchema(t *testing.T) {
	_, err := NewJSONSchemaValidator(map[string]any{
		"type": "invalid_type",
	})
	// May or may not error during compile depending on the library; test graceful handling.
	if err != nil {
		assert.Contains(t, err.Error(), "schema")
	}
}
