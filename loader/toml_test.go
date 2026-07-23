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

func TestTOMLLoader_Load(t *testing.T) {
	l := NewTOML("testdata/simple.toml")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, int64(5432), db["port"])
}

// TestTOMLLoader_MissingFile asserts the post-G07 default-Raise contract:
// a missing file must surface a typed *confii.ConfigError. The legacy
// (nil, nil) behavior is now opt-in via WithTOMLErrorPolicy(Ignore).
func TestTOMLLoader_MissingFile(t *testing.T) {
	l := NewTOML("testdata/nonexistent.toml")
	result, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))

	// Opt back into the legacy graceful-absence behavior.
	l = NewTOML("testdata/nonexistent.toml",
		WithTOMLErrorPolicy(confii.ErrorPolicyIgnore),
	)
	result, err = l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

// TestTOMLLoader_MissingFile_Policies covers G07.
func TestTOMLLoader_MissingFile_Policies(t *testing.T) {
	missing := "testdata/nonexistent.toml"

	t.Run("raise", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewTOML(missing,
			WithTOMLErrorPolicy(confii.ErrorPolicyRaise),
			WithTOMLLogger(logger),
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
		l := NewTOML(missing,
			WithTOMLErrorPolicy(confii.ErrorPolicyWarn),
			WithTOMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		out := logBuf.String()
		assert.Contains(t, out, "level=WARN")
		assert.Contains(t, out, "toml: source file missing")
		assert.Contains(t, out, missing)
	})

	t.Run("ignore", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewTOML(missing,
			WithTOMLErrorPolicy(confii.ErrorPolicyIgnore),
			WithTOMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.Empty(t, logBuf.String())
	})
}

// TestTOMLLoader_DefaultPolicyIsRaise verifies the default-Raise contract.
func TestTOMLLoader_DefaultPolicyIsRaise(t *testing.T) {
	l := NewTOML("testdata/nonexistent.toml")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}

// TestTOMLLoader_NilLoggerIgnored ensures WithTOMLLogger(nil) is a no-op.
func TestTOMLLoader_NilLoggerIgnored(t *testing.T) {
	l := NewTOML("testdata/nonexistent.toml",
		WithTOMLErrorPolicy(confii.ErrorPolicyWarn),
		WithTOMLLogger(nil),
	)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
}

// TestTOMLLoader_PresentFileUnaffectedByPolicy confirms the policy only
// affects error paths.
func TestTOMLLoader_PresentFileUnaffectedByPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.toml")
	require.NoError(t, os.WriteFile(path, []byte("a = 1\n"), 0o600))

	for _, p := range []confii.ErrorPolicy{
		confii.ErrorPolicyRaise,
		confii.ErrorPolicyWarn,
		confii.ErrorPolicyIgnore,
	} {
		l := NewTOML(path, WithTOMLErrorPolicy(p))
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Equal(t, int64(1), result["a"])
	}
}
