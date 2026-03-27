package loader

import (
	"context"
	"errors"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantNil bool
		wantErr bool
		errType error
	}{
		{
			name: "valid yaml",
			path: "testdata/simple.yaml",
		},
		{
			name:    "missing file returns nil",
			path:    "testdata/nonexistent.yaml",
			wantNil: true,
		},
		{
			name:    "invalid yaml returns format error",
			path:    "testdata/invalid.yaml",
			wantErr: true,
			errType: confii.ErrConfigFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewYAML(tt.path)
			assert.Equal(t, tt.path, l.Source())

			result, err := l.Load(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.True(t, errors.Is(err, tt.errType), "expected %v, got %v", tt.errType, err)
				}
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
				return
			}

			// Verify content.
			db, ok := result["database"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "localhost", db["host"])
			assert.Equal(t, 5432, db["port"])
		})
	}
}
