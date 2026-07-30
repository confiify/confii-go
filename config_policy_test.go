// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureLoggerOpts() (*bytes.Buffer, []Option) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return buf, []Option{WithLogger(logger)}
}

func TestConfig_LoaderError_RaisePolicy_Surfaces(t *testing.T) {
	bad := &stubLoader{source: "bad", err: errors.New("boom")}
	_, err := NewWithContext[any](context.Background(),
		WithLoaders(bad),
		WithOnError(ErrorPolicyRaise),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestConfig_LoaderError_WarnPolicy_LogsAndContinues(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()
	bad := &stubLoader{source: "bad", err: errors.New("warn-me")}
	good := &stubLoader{source: "good", data: map[string]any{"k": "v"}}

	opts := append([]Option{
		WithLoaders(bad, good),
		WithOnError(ErrorPolicyWarn),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	got, _ := cfg.Get("k")
	assert.Equal(t, "v", got)

	out := logBuf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "loader error")
	assert.Contains(t, out, "warn-me")
}

func TestConfig_LoaderError_IgnorePolicy_SilentlyContinues(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()
	bad := &stubLoader{source: "bad", err: errors.New("hush")}
	good := &stubLoader{source: "good", data: map[string]any{"k": "v"}}

	opts := append([]Option{
		WithLoaders(bad, good),
		WithOnError(ErrorPolicyIgnore),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	got, _ := cfg.Get("k")
	assert.Equal(t, "v", got)

	assert.NotContains(t, logBuf.String(), "loader error",
		"ErrorPolicyIgnore must not produce a 'loader error' log record ")
	assert.NotContains(t, logBuf.String(), "hush",
		"ErrorPolicyIgnore must not leak the underlying error message ")
}

func writeYAMLFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

const composeFailingYAML = "" +
	"_include:\n" +
	"  - this_file_does_not_exist_for_g07.yaml\n" +
	"app:\n" +
	"  name: raw\n"

func TestConfig_CompositionError_RaisePolicy_SurfacesError(t *testing.T) {
	path := writeYAMLFile(t, composeFailingYAML)
	_, err := NewWithContext[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
		WithOnError(ErrorPolicyRaise),
	)
	require.Error(t, err, "composer error must surface under Raise ")
}

func TestConfig_CompositionError_WarnPolicy_LogsAndContinues(t *testing.T) {
	path := writeYAMLFile(t, composeFailingYAML)
	logBuf, logOpts := captureLoggerOpts()
	opts := append([]Option{
		WithLoaders(&fileAutoLoader{path: path}),
		WithOnError(ErrorPolicyWarn),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	got, _ := cfg.Get("app.name")
	assert.Equal(t, "raw", got)

	out := logBuf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "composition error")
}

func TestConfig_CompositionError_IgnorePolicy_SilentlyContinues(t *testing.T) {
	path := writeYAMLFile(t, composeFailingYAML)
	logBuf, logOpts := captureLoggerOpts()
	opts := append([]Option{
		WithLoaders(&fileAutoLoader{path: path}),
		WithOnError(ErrorPolicyIgnore),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	got, _ := cfg.Get("app.name")
	assert.Equal(t, "raw", got)

	assert.NotContains(t, logBuf.String(), "composition error",
		"ErrorPolicyIgnore must not produce a 'composition error' log record ")
}

func TestParseErrorPolicy_AcceptsValidStrings(t *testing.T) {
	cases := map[string]ErrorPolicy{
		"raise":   ErrorPolicyRaise,
		"warn":    ErrorPolicyWarn,
		"ignore":  ErrorPolicyIgnore,
		"RAISE":   ErrorPolicyRaise,
		"  Warn ": ErrorPolicyWarn,
	}
	for in, want := range cases {
		got, err := ParseErrorPolicy(in)
		require.NoError(t, err, "input %q", in)
		assert.Equal(t, want, got, "input %q", in)
	}
}

func TestParseErrorPolicy_RejectsUnknownStrings(t *testing.T) {
	cases := []string{"", "warning", "fatal", "yes", "RAIS"}
	for _, in := range cases {
		_, err := ParseErrorPolicy(in)
		require.Error(t, err, "input %q should be rejected", in)
		var ce *ConfigError
		require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
		assert.True(t, errors.Is(err, ErrConfigLoad))

		assert.Contains(t, err.Error(), fmt.Sprintf("%q", in))
	}
}

func withTempCWD(t *testing.T) string {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(orig)
		selfconfig.ClearCache()
	})
	selfconfig.ClearCache()
	return dir
}

func TestSelfConfig_InvalidOnErrorString_RejectedWithTypedError(t *testing.T) {
	dir := withTempCWD(t)
	yamlPath := filepath.Join(dir, "confii.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("on_error: warning\n"), 0o600))

	_, err := NewWithContext[any](context.Background())
	require.Error(t, err, "invalid on_error string must be rejected ")
	var ce *ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.True(t, errors.Is(err, ErrConfigLoad))
	assert.Contains(t, err.Error(), "warning")
}

func TestSelfConfig_ValidOnErrorString_Accepted(t *testing.T) {
	dir := withTempCWD(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte("on_error: warn\n"), 0o600))

	logBuf, logOpts := captureLoggerOpts()
	opts := append([]Option{
		WithLoaders(&stubLoader{source: "bad", err: errors.New("boom")}),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, strings.Contains(logBuf.String(), "loader error"),
		"self-config on_error=warn should activate Warn semantics")
}

func TestSelfConfig_ExplicitOptionTrumpsSelfConfigOnError(t *testing.T) {
	dir := withTempCWD(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte("on_error: warning\n"), 0o600))

	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&stubLoader{source: "ok", data: map[string]any{"k": "v"}}),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)
	require.NotNil(t, cfg)
}
