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
	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilder_Basic(t *testing.T) {
	cfg, err := confii.NewBuilder[any]().
		WithEnv("production").
		AddLoader(loader.NewYAML("loader/testdata/envs.yaml")).
		BuildWithContext(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "production", cfg.Env())

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)
}

func TestBuilder_MultipleLoaders(t *testing.T) {
	cfg, err := confii.NewBuilder[any]().
		AddLoader(loader.NewYAML("loader/testdata/simple.yaml")).
		AddLoader(loader.NewJSON("loader/testdata/simple.json")).
		BuildWithContext(context.Background())

	require.NoError(t, err)
	assert.True(t, cfg.Has("database.host"))
}

func TestBuilder_FreezeOnLoad(t *testing.T) {
	cfg, err := confii.NewBuilder[any]().
		AddLoader(loader.NewYAML("loader/testdata/simple.yaml")).
		EnableFreezeOnLoad().
		BuildWithContext(context.Background())

	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())
}

type BuilderTestConfig struct {
	Database struct {
		Host string `confii:"host" validate:"required"`
		Port int    `confii:"port" validate:"required"`
		Name string `confii:"name" validate:"required"`
	} `confii:"database"`
	Debug bool `confii:"debug"`
}

func TestBuilder_WithTypedAccess(t *testing.T) {
	cfg, err := confii.NewBuilder[BuilderTestConfig]().
		AddLoader(loader.NewYAML("loader/testdata/simple.yaml")).
		BuildWithContext(context.Background())

	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "localhost", model.Database.Host)
	assert.Equal(t, 5432, model.Database.Port)
	assert.True(t, model.Debug)
}

type stubBuilderLoader struct {
	source string
	data   map[string]any
}

func (s *stubBuilderLoader) Load(_ context.Context) (map[string]any, error) {
	return s.data, nil
}

func (s *stubBuilderLoader) Source() string { return s.source }

func chdirToTempDir(t *testing.T, dir string) {
	t.Helper()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
		selfconfig.ClearCache()
	})
	selfconfig.ClearCache()
}

func TestBuilder_AddLoader_MarksLoadersExplicit_PreventsSelfConfigAppend(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "sources:\n  - type: yaml\n    path: selfconfig.yaml\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(confiiYAML), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "selfconfig.yaml"), []byte("from_selfconfig: true\nshared: from_selfconfig\n"), 0644))

	chdirToTempDir(t, dir)

	stub := &stubBuilderLoader{
		source: "stub://builder",
		data:   map[string]any{"from_stub": true, "shared": "from_stub"},
	}

	cfg, err := confii.NewBuilder[any]().
		AddLoader(stub).
		BuildWithContext(context.Background())
	require.NoError(t, err)

	assert.True(t, cfg.Has("from_stub"), "expected stub loader's keys to be present")

	assert.False(t, cfg.Has("from_selfconfig"), "self-config sources leaked past explicit AddLoader")

	got, err := cfg.Get("shared")
	require.NoError(t, err)
	assert.Equal(t, "from_stub", got, "stub loader must be the only contributor when AddLoader is used")
}

func TestBuilder_NoLoaders_LeavesSelfConfigDefaultsActive(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "sources:\n  - type: yaml\n    path: selfconfig.yaml\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(confiiYAML), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "selfconfig.yaml"), []byte("from_selfconfig: true\n"), 0644))

	chdirToTempDir(t, dir)

	cfg, err := confii.NewBuilder[any]().BuildWithContext(context.Background())
	require.NoError(t, err)

	assert.True(t, cfg.Has("from_selfconfig"), "self-config sources should be honored when Builder has no AddLoader call")
}
