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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confii "github.com/confiify/confii-go/v2"
)

func TestEnvFileLoader_Load(t *testing.T) {
	l := NewEnvFile("testdata/simple.env")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "localhost", result["HOST"])
	assert.Equal(t, 5432, result["PORT"])
	assert.Equal(t, true, result["DEBUG"])
	assert.Equal(t, "my app", result["NAME"])
	assert.Equal(t, "raw_value", result["SECRET"])
	assert.Equal(t, "some_value", result["INLINE"])

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "db-server", db["host"])
}

func TestEnvFileLoaderRejectsJSONDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	require.NoError(t, os.WriteFile(path, []byte(`{"server":{"port":8080}}`), 0o600))
	_, err := NewEnvFile(path).Load(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigFormat)
}

func TestEnvFileLoader_MissingFile(t *testing.T) {
	l := NewEnvFile("testdata/nonexistent.env")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnvFileLoader_DefaultPath(t *testing.T) {
	l := NewEnvFile("")
	assert.Equal(t, ".env", l.Source())
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestEnvFileLoader_MalformedLinePolicies(t *testing.T) {

	const content = "GOOD=1\n" +
		"NOEQUALS\n" +
		"AFTER=ok\n" +
		"=novalueoneitherside\n" +
		"trailing whitespace and no equals \n"

	cases := []struct {
		name   string
		policy confii.ErrorPolicy
	}{
		{"raise", confii.ErrorPolicyRaise},
		{"warn", confii.ErrorPolicyWarn},
		{"ignore", confii.ErrorPolicyIgnore},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEnvFile(t, content)

			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			l := NewEnvFile(path,
				WithEnvFileErrorPolicy(tc.policy),
				WithEnvFileLogger(logger),
			)
			result, err := l.Load(context.Background())

			switch tc.policy {
			case confii.ErrorPolicyRaise:
				require.Error(t, err)

				var ce *confii.ConfigError
				require.True(t, errors.As(err, &ce), "expected *confii.ConfigError, got %T", err)
				assert.Equal(t, path, ce.Source)

				assert.True(t, errors.Is(err, confii.ErrConfigLoad))
				assert.Contains(t, err.Error(), "line 2")
				assert.Contains(t, err.Error(), "NOEQUALS")

				assert.Nil(t, result)

				assert.Empty(t, logBuf.String())

			case confii.ErrorPolicyWarn:
				require.NoError(t, err)
				require.NotNil(t, result)

				out := logBuf.String()
				assert.Contains(t, out, `level=WARN`)
				assert.Contains(t, out, "envfile: malformed line skipped")
				assert.Contains(t, out, "line=2")
				assert.Contains(t, out, "line=5")
				assert.Contains(t, out, path)

				assert.Equal(t, 1, result["GOOD"])
				assert.Equal(t, "ok", result["AFTER"])

				assert.Equal(t, "novalueoneitherside", result[""])

			case confii.ErrorPolicyIgnore:
				require.NoError(t, err)
				require.NotNil(t, result)

				assert.Empty(t, logBuf.String())
				assert.Equal(t, 1, result["GOOD"])
				assert.Equal(t, "ok", result["AFTER"])
			}
		})
	}
}

func TestEnvFileLoader_DefaultPolicyIsRaise(t *testing.T) {
	path := writeEnvFile(t, "OK=1\nNOEQUALS\n")

	l := NewEnvFile(path)
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
	assert.Contains(t, err.Error(), "line 2")
}

func TestEnvFileLoader_RegressionSilentDropClosed(t *testing.T) {
	path := writeEnvFile(t, "OK=1\nNOEQUALS\n")

	_, err := NewEnvFile(path, WithEnvFileErrorPolicy(confii.ErrorPolicyRaise)).
		Load(context.Background())
	require.Error(t, err, "Raise must not silently drop malformed lines ")

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	_, err = NewEnvFile(path,
		WithEnvFileErrorPolicy(confii.ErrorPolicyWarn),
		WithEnvFileLogger(logger),
	).Load(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, logBuf.String(), "Warn must emit a log record for malformed lines ")
	assert.True(t, strings.Contains(logBuf.String(), "malformed line skipped"))

	logBuf.Reset()
	_, err = NewEnvFile(path,
		WithEnvFileErrorPolicy(confii.ErrorPolicyIgnore),
		WithEnvFileLogger(logger),
	).Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, logBuf.String())
}

func TestEnvFileLoader_NilLoggerIgnored(t *testing.T) {
	path := writeEnvFile(t, "NOEQUALS\n")
	l := NewEnvFile(path,
		WithEnvFileErrorPolicy(confii.ErrorPolicyWarn),
		WithEnvFileLogger(nil),
	)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
}
