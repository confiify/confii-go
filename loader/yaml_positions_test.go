// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader_test

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

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestYAMLLoader_ImplementsPositionalLoader(t *testing.T) {
	var l any = loader.NewYAML("config.yaml")
	_, ok := l.(confii.PositionalLoader)
	assert.True(t, ok, "YAMLLoader must satisfy confii.PositionalLoader")
}

func TestYAMLLoader_PositionsReportKeyLines(t *testing.T) {
	path := writeYAML(t, "database:\n  host: localhost\n  port: 5432\napp:\n  name: svc\n")
	l := loader.NewYAML(path)

	_, err := l.Load(context.Background())
	require.NoError(t, err)

	positions := l.Positions()
	assert.Equal(t, 2, positions["database.host"])
	assert.Equal(t, 3, positions["database.port"])
	assert.Equal(t, 5, positions["app.name"])
}

func TestYAMLLoader_PositionsEmptyBeforeLoad(t *testing.T) {
	l := loader.NewYAML(writeYAML(t, "a: 1\n"))
	assert.Empty(t, l.Positions(), "no positions are known until Load runs")
}

func TestYAMLLoader_PositionsAreDefensivelyCopied(t *testing.T) {
	l := loader.NewYAML(writeYAML(t, "a: 1\n"))
	_, err := l.Load(context.Background())
	require.NoError(t, err)

	first := l.Positions()
	require.Equal(t, 1, first["a"])
	first["a"] = 999
	delete(first, "a")

	assert.Equal(t, 1, l.Positions()["a"],
		"mutating a returned map must not affect the loader")
}

func TestYAMLLoader_PositionsRefreshOnReload(t *testing.T) {
	path := writeYAML(t, "a: 1\n")
	l := loader.NewYAML(path)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, l.Positions()["a"])

	require.NoError(t, os.WriteFile(path, []byte("# moved down\n\na: 1\n"), 0o600))
	_, err = l.Load(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 3, l.Positions()["a"], "positions must reflect the latest load")
}

// YAMLLoader values were comparable before positions were tracked, so callers
// may use == or a map key. Storing the index behind a pointer-shaped field
// keeps that property; a plain map or a mutex-plus-map field would silently
// break it, which `make api-compat` rejects.
func TestYAMLLoader_RemainsComparable(t *testing.T) {
	var a, b loader.YAMLLoader
	assert.True(t, a == b, "zero YAMLLoader values must stay comparable")
}
