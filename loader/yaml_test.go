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

func TestYAMLLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		opts    []YAMLOption
		wantNil bool
		wantErr bool
		errType error
	}{
		{
			name: "valid yaml",
			path: "testdata/simple.yaml",
		},
		{
			name:    "missing file under default policy raises",
			path:    "testdata/nonexistent.yaml",
			wantErr: true,
			errType: confii.ErrConfigLoad,
		},
		{
			name:    "missing file with WithYAMLErrorPolicy(Ignore) returns nil",
			path:    "testdata/nonexistent.yaml",
			opts:    []YAMLOption{WithYAMLErrorPolicy(confii.ErrorPolicyIgnore)},
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
			l := NewYAML(tt.path, tt.opts...)
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

			db, ok := result["database"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "localhost", db["host"])
			assert.Equal(t, 5432, db["port"])
		})
	}
}

func TestYAMLLoaderRejectsJSONDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`{"server":{"port":8080}}`), 0o600))

	_, err := NewYAML(path).Load(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigFormat)
	assert.Contains(t, err.Error(), "JSON document")
}

func TestYAMLLoader_MissingFile_Policies(t *testing.T) {
	missing := "testdata/nonexistent.yaml"

	t.Run("raise", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewYAML(missing,
			WithYAMLErrorPolicy(confii.ErrorPolicyRaise),
			WithYAMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.Error(t, err)
		assert.Nil(t, result)
		var ce *confii.ConfigError
		require.True(t, errors.As(err, &ce), "expected *confii.ConfigError, got %T", err)
		assert.Equal(t, missing, ce.Source)
		assert.True(t, errors.Is(err, confii.ErrConfigLoad))

		assert.Empty(t, logBuf.String())
	})

	t.Run("warn", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewYAML(missing,
			WithYAMLErrorPolicy(confii.ErrorPolicyWarn),
			WithYAMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		out := logBuf.String()
		assert.Contains(t, out, "level=WARN")
		assert.Contains(t, out, "yaml: source file missing")
		assert.Contains(t, out, missing)
	})

	t.Run("ignore", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewYAML(missing,
			WithYAMLErrorPolicy(confii.ErrorPolicyIgnore),
			WithYAMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.Empty(t, logBuf.String())
	})
}

func TestYAMLLoader_DefaultPolicyIsRaise(t *testing.T) {
	l := NewYAML("testdata/nonexistent.yaml")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}

func TestYAMLLoader_NilLoggerIgnored(t *testing.T) {
	l := NewYAML("testdata/nonexistent.yaml",
		WithYAMLErrorPolicy(confii.ErrorPolicyWarn),
		WithYAMLLogger(nil),
	)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
}

func TestYAMLLoader_NormalizesNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ports.yaml")
	body := "" +
		"ports:\n" +
		"  80: http\n" +
		"  443: https\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	ports, ok := result["ports"].(map[string]any)
	require.True(t, ok, "expected ports to be map[string]any, got %T", result["ports"])
	assert.Equal(t, "http", ports["80"])
	assert.Equal(t, "https", ports["443"])
}

func TestYAMLLoader_NormalizesNestedNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested.yaml")
	body := "" +
		"services:\n" +
		"  web:\n" +
		"    ports:\n" +
		"      80: http\n" +
		"      443: https\n" +
		"    flags:\n" +
		"      true: enabled\n" +
		"      false: disabled\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	services, ok := result["services"].(map[string]any)
	require.True(t, ok, "services should be map[string]any, got %T", result["services"])
	web, ok := services["web"].(map[string]any)
	require.True(t, ok, "services.web should be map[string]any, got %T", services["web"])
	ports, ok := web["ports"].(map[string]any)
	require.True(t, ok, "services.web.ports should be map[string]any, got %T", web["ports"])
	assert.Equal(t, "http", ports["80"])
	assert.Equal(t, "https", ports["443"])
	flags, ok := web["flags"].(map[string]any)
	require.True(t, ok, "services.web.flags should be map[string]any, got %T", web["flags"])
	assert.Equal(t, "enabled", flags["true"])
	assert.Equal(t, "disabled", flags["false"])
}

func TestYAMLLoader_StringKeysUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.yaml")
	body := "" +
		"database:\n" +
		"  host: localhost\n" +
		"  port: 5432\n" +
		"  ssl: true\n" +
		"tags:\n" +
		"  - alpha\n" +
		"  - beta\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, 5432, db["port"])
	assert.Equal(t, true, db["ssl"])

	tags, ok := result["tags"].([]any)
	require.True(t, ok, "tags should be []any, got %T", result["tags"])
	assert.Equal(t, []any{"alpha", "beta"}, tags)
}

func TestYAMLLoader_AnchorsAliasesAndMergeKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anchors.yaml")
	body := "" +
		"defaults: &defaults\n" +
		"  host: localhost\n" +
		"  port: 5432\n" +
		"  pool:\n" +
		"    min: 1\n" +
		"    max: 5\n" +
		"development:\n" +
		"  <<: *defaults\n" +
		"  host: dev-db.local\n" +
		"production:\n" +
		"  <<: *defaults\n" +
		"  pool:\n" +
		"    max: 20\n" +
		"shared: &shared\n" +
		"  retries: 3\n" +
		"service_a: *shared\n" +
		"service_b: *shared\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	development, ok := result["development"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "dev-db.local", development["host"])
	assert.Equal(t, 5432, development["port"])
	devPool, ok := development["pool"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, devPool["min"])
	assert.Equal(t, 5, devPool["max"])

	production, ok := result["production"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", production["host"])
	assert.Equal(t, 5432, production["port"])
	prodPool, ok := production["pool"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 20, prodPool["max"])

	serviceA, ok := result["service_a"].(map[string]any)
	require.True(t, ok)
	serviceB, ok := result["service_b"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, serviceA["retries"])
	assert.Equal(t, 3, serviceB["retries"])

	serviceA["retries"] = 9
	assert.Equal(t, 3, serviceB["retries"], "normalized YAML aliases must not share mutable maps")
}

func TestYAMLLoader_SliceWithMapElements(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "list.yaml")
	body := "" +
		"items:\n" +
		"  - 1: one\n" +
		"    2: two\n" +
		"  - 3: three\n" +
		"    4: four\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	items, ok := result["items"].([]any)
	require.True(t, ok, "items should be []any, got %T", result["items"])
	require.Len(t, items, 2)

	first, ok := items[0].(map[string]any)
	require.True(t, ok, "items[0] should be map[string]any, got %T", items[0])
	assert.Equal(t, "one", first["1"])
	assert.Equal(t, "two", first["2"])

	second, ok := items[1].(map[string]any)
	require.True(t, ok, "items[1] should be map[string]any, got %T", items[1])
	assert.Equal(t, "three", second["3"])
	assert.Equal(t, "four", second["4"])
}

func TestYAMLLoader_PresentFileUnaffectedByPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: 1\n"), 0o600))

	for _, p := range []confii.ErrorPolicy{
		confii.ErrorPolicyRaise,
		confii.ErrorPolicyWarn,
		confii.ErrorPolicyIgnore,
	} {
		l := NewYAML(path, WithYAMLErrorPolicy(p))
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, result["a"])
	}
}
