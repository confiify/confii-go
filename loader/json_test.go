package loader

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		opts    []JSONOption
		wantNil bool
		wantErr bool
		errType error
	}{
		{
			name: "valid json",
			path: "testdata/simple.json",
		},
		{
			// G07: post-fix the default policy is Raise.
			name:    "missing file under default policy raises",
			path:    "testdata/nonexistent.json",
			wantErr: true,
			errType: confii.ErrConfigLoad,
		},
		{
			name:    "missing file with WithJSONErrorPolicy(Ignore) returns nil",
			path:    "testdata/nonexistent.json",
			opts:    []JSONOption{WithJSONErrorPolicy(confii.ErrorPolicyIgnore)},
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
			l := NewJSON(tt.path, tt.opts...)
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

// TestJSONLoader_MissingFile_Policies covers G07.
func TestJSONLoader_MissingFile_Policies(t *testing.T) {
	missing := "testdata/nonexistent.json"

	t.Run("raise", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewJSON(missing,
			WithJSONErrorPolicy(confii.ErrorPolicyRaise),
			WithJSONLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.Error(t, err)
		assert.Nil(t, result)
		var ce *confii.ConfigError
		require.True(t, errors.As(err, &ce))
		assert.Equal(t, missing, ce.Source)
		assert.True(t, errors.Is(err, confii.ErrConfigLoad))
		assert.Empty(t, logBuf.String())
	})

	t.Run("warn", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewJSON(missing,
			WithJSONErrorPolicy(confii.ErrorPolicyWarn),
			WithJSONLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		out := logBuf.String()
		assert.Contains(t, out, "level=WARN")
		assert.Contains(t, out, "json: source file missing")
		assert.Contains(t, out, missing)
	})

	t.Run("ignore", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewJSON(missing,
			WithJSONErrorPolicy(confii.ErrorPolicyIgnore),
			WithJSONLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.Empty(t, logBuf.String())
	})
}

// TestJSONLoader_DefaultPolicyIsRaise verifies the default-Raise contract.
func TestJSONLoader_DefaultPolicyIsRaise(t *testing.T) {
	l := NewJSON("testdata/nonexistent.json")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}

// TestJSONLoader_NilLoggerIgnored ensures WithJSONLogger(nil) is a no-op.
func TestJSONLoader_NilLoggerIgnored(t *testing.T) {
	l := NewJSON("testdata/nonexistent.json",
		WithJSONErrorPolicy(confii.ErrorPolicyWarn),
		WithJSONLogger(nil),
	)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
}

// TestJSONLoader_PresentFileUnaffectedByPolicy confirms the policy only
// affects error paths.
func TestJSONLoader_PresentFileUnaffectedByPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"a": 1}`), 0o600))

	for _, p := range []confii.ErrorPolicy{
		confii.ErrorPolicyRaise,
		confii.ErrorPolicyWarn,
		confii.ErrorPolicyIgnore,
	} {
		l := NewJSON(path, WithJSONErrorPolicy(p))
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Equal(t, float64(1), result["a"])
	}
}
