// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestINILoader_Load(t *testing.T) {
	l := NewINI("testdata/simple.ini")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, 5432, db["port"])
}

func TestINILoaderRejectsJSONDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.ini")
	require.NoError(t, os.WriteFile(path, []byte(`{"server":{"port":8080}}`), 0o600))
	_, err := NewINI(path).Load(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigFormat)
}

func TestINILoader_MissingFile(t *testing.T) {
	l := NewINI("testdata/nonexistent.ini")
	result, err := l.Load(context.Background())
	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))

	l = NewINI("testdata/nonexistent.ini",
		WithINIErrorPolicy(confii.ErrorPolicyIgnore),
	)
	result, err = l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestINILoader_MissingFile_Policies(t *testing.T) {
	missing := "testdata/nonexistent.ini"

	t.Run("raise", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewINI(missing,
			WithINIErrorPolicy(confii.ErrorPolicyRaise),
			WithINILogger(logger),
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
		l := NewINI(missing,
			WithINIErrorPolicy(confii.ErrorPolicyWarn),
			WithINILogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		out := logBuf.String()
		assert.Contains(t, out, "level=WARN")
		assert.Contains(t, out, "ini: source file missing")
		assert.Contains(t, out, missing)
	})

	t.Run("ignore", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewINI(missing,
			WithINIErrorPolicy(confii.ErrorPolicyIgnore),
			WithINILogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.Empty(t, logBuf.String())
	})
}

func TestINILoader_DefaultPolicyIsRaise(t *testing.T) {
	l := NewINI("testdata/nonexistent.ini")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}

func TestINILoader_NilLoggerIgnored(t *testing.T) {
	l := NewINI("testdata/nonexistent.ini",
		WithINIErrorPolicy(confii.ErrorPolicyWarn),
		WithINILogger(nil),
	)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
}

func TestINILoader_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	emptyFile := filepath.Join(dir, "empty.ini")
	require.NoError(t, os.WriteFile(emptyFile, []byte(""), 0644))

	l := NewINI(emptyFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)

	assert.Nil(t, result)
}

func TestINILoader_DefaultsOnlyFile_PopulatesRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "defaults_only.ini")
	contents := "host = localhost\nport = 5432\ndebug = true\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0644))

	l := NewINI(path)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "localhost", result["host"])
	assert.Equal(t, 5432, result["port"])
	assert.Equal(t, true, result["debug"])
}

func TestINILoader_MixedDefaultsAndSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.ini")
	contents := "" +
		"app_name = confii\n" +
		"version = 2\n" +
		"\n" +
		"[database]\n" +
		"host = db.example.com\n" +
		"port = 5432\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0644))

	l := NewINI(path)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "confii", result["app_name"])
	assert.Equal(t, 2, result["version"])

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "db.example.com", db["host"])
	assert.Equal(t, 5432, db["port"])
}
