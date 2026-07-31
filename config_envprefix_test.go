// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithEnvPrefix_AppendsEnvironmentLoader(t *testing.T) {
	t.Setenv("APP_HOST", "example.com")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("APP"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err,
		":APP_HOST must be visible via the loader pipeline when "+
			"WithEnvPrefix is set (no sysenv-fallback enabled)")
	assert.Equal(t, "example.com", host)
}

func TestWithEnvPrefix_NestedDoubleUnderscore(t *testing.T) {
	t.Setenv("APP_DB__HOST", "postgres.example.com")
	t.Setenv("APP_DB__PORT", "5432")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("APP"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("db.host")
	require.NoError(t, err)
	assert.Equal(t, "postgres.example.com", host,
		":APP_DB__HOST must split on `__` and surface as db.host")

	port, err := cfg.Get("db.port")
	require.NoError(t, err)
	assert.EqualValues(t, 5432, port,
		":APP_DB__PORT must surface as a coerced numeric value")
}

func TestWithEnvPrefix_LayersIncludesEnvironmentLoader(t *testing.T) {
	t.Setenv("APP_PRESENT", "yes")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("APP"),
	)
	require.NoError(t, err)

	layers := cfg.Layers()
	var found bool
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			found = true
			break
		}
	}
	assert.True(t, found,
		":cfg.Layers must include the auto-installed env loader "+
			"under the canonical `environment:APP` source identifier; "+
			"got layers: %v", layers)
}

func TestWithEnvPrefix_NoDoubleApplyWithExplicitLoader(t *testing.T) {
	t.Setenv("APP_HOST", "example.com")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewEnvironment("APP")),
		confii.WithEnvPrefix("APP"),
	)
	require.NoError(t, err)

	layers := cfg.Layers()
	envLayerCount := 0
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			envLayerCount++
		}
	}
	assert.Equal(t, 1, envLayerCount,
		":env-loader layer must appear exactly once when both "+
			"WithLoaders(loader.NewEnvironment(\"APP\")) and "+
			"WithEnvPrefix(\"APP\") are supplied; got %d", envLayerCount)

	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "example.com", host)
}

func TestWithEnvPrefixOptionOrderDoesNotLoseAutoLoader(t *testing.T) {
	t.Setenv("APP_HOST", "from-environment")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("APP"),
		confii.WithLoaders(valueLoader{source: "file", data: map[string]any{"host": "from-file"}}),
	)
	require.NoError(t, err)
	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "from-environment", host)

	layers := cfg.Layers()
	require.Len(t, layers, 2)
	assert.Equal(t, "environment:APP", layers[1]["source"])
}

func TestRegressionBaseline_WithoutFix_FallbackOnly(t *testing.T) {
	t.Setenv("APP_DB__SOCKET", "/tmp/g03.sock")

	require.NoError(t, os.Unsetenv("APP_DB_SOCKET"))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("APP"),
	)
	require.NoError(t, err)

	socket, err := cfg.Get("db.socket")
	require.NoError(t, err,
		" regression:APP_DB__SOCKET must surface via the env loader "+
			"in the load+merge pipeline, not via sysenv fallback")
	assert.Equal(t, "/tmp/g03.sock", socket)

	layers := cfg.Layers()
	var envLayer map[string]any
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			envLayer = layer
			break
		}
	}
	require.NotNil(t, envLayer,
		" regression:env loader layer must exist post-fix")
	keyCount, _ := envLayer["key_count"].(int)
	assert.Greater(t, keyCount, 0,
		" regression:env loader layer must report >0 keys; got %d", keyCount)
}
