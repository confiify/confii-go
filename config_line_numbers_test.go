// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfigFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestSourceInfo_LineNumberReportedForYAML(t *testing.T) {
	path := writeConfigFile(t, "config.yaml",
		"database:\n  host: localhost\n  port: 5432\napp:\n  name: svc\n")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	info := cfg.GetSourceInfo("database.host")
	require.NotNil(t, info)
	assert.Equal(t, 2, info.LineNumber,
		"database.host is defined on line 2 of the YAML source")

	info = cfg.GetSourceInfo("app.name")
	require.NotNil(t, info)
	assert.Equal(t, 5, info.LineNumber)
}

func TestSourceInfo_LineNumberZeroForFormatsWithoutPositions(t *testing.T) {
	path := writeConfigFile(t, "config.json", `{"database":{"host":"localhost"}}`)

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewJSON(path)),
	)
	require.NoError(t, err)

	info := cfg.GetSourceInfo("database.host")
	require.NotNil(t, info)
	assert.Equal(t, 0, info.LineNumber,
		"a loader that cannot report positions leaves the line unknown")
}

func TestSourceInfo_LineNumberFollowsOverridingLayer(t *testing.T) {
	base := writeConfigFile(t, "base.yaml", "app:\n  port: 8080\n")
	over := writeConfigFile(t, "over.yaml", "# comment\n# comment\napp:\n  port: 9090\n")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(base), loader.NewYAML(over)),
	)
	require.NoError(t, err)

	info := cfg.GetSourceInfo("app.port")
	require.NotNil(t, info)
	assert.Equal(t, 9090, info.Value)
	assert.Equal(t, 4, info.LineNumber,
		"the line must point at the layer that supplied the effective value")
}

func TestSourceInfo_LineNumberSurvivesMixedFormatLayers(t *testing.T) {
	yamlPath := writeConfigFile(t, "base.yaml", "app:\n  name: from-yaml\n")
	jsonPath := writeConfigFile(t, "over.json", `{"app":{"other":"from-json"}}`)

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath), loader.NewJSON(jsonPath)),
	)
	require.NoError(t, err)

	info := cfg.GetSourceInfo("app.name")
	require.NotNil(t, info)
	assert.Equal(t, 2, info.LineNumber,
		"a positionless layer must not erase a known line for an untouched key")
}
