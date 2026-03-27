package loader

import (
	"context"
	"errors"
	"testing"

	confii "github.com/qualitycoe/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantNil bool
		wantErr bool
		errType error
	}{
		{
			name: "valid json",
			path: "testdata/simple.json",
		},
		{
			name:    "missing file returns nil",
			path:    "testdata/nonexistent.json",
			wantNil: true,
		},
		{
			name:    "invalid json returns format error",
			path:    "testdata/invalid.json",
			wantErr: true,
			errType: confii.ErrConfigFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewJSON(tt.path)
			result, err := l.Load(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.True(t, errors.Is(err, tt.errType))
				}
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
				return
			}

			db, ok := result["database"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "localhost", db["host"])
			// JSON numbers are float64.
			assert.Equal(t, float64(5432), db["port"])
		})
	}
}
