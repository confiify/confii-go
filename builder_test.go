package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilder_Basic(t *testing.T) {
	cfg, err := confii.NewBuilder[any]().
		WithEnv("production").
		AddLoader(loader.NewYAML("loader/testdata/envs.yaml")).
		Build(context.Background())

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
		Build(context.Background())

	require.NoError(t, err)
	assert.True(t, cfg.Has("database.host"))
}

func TestBuilder_FreezeOnLoad(t *testing.T) {
	cfg, err := confii.NewBuilder[any]().
		AddLoader(loader.NewYAML("loader/testdata/simple.yaml")).
		EnableFreezeOnLoad().
		Build(context.Background())

	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())
}

type BuilderTestConfig struct {
	Database struct {
		Host string `mapstructure:"host" validate:"required"`
		Port int    `mapstructure:"port" validate:"required"`
		Name string `mapstructure:"name" validate:"required"`
	} `mapstructure:"database"`
	Debug bool `mapstructure:"debug"`
}

func TestBuilder_WithTypedAccess(t *testing.T) {
	cfg, err := confii.NewBuilder[BuilderTestConfig]().
		AddLoader(loader.NewYAML("loader/testdata/simple.yaml")).
		Build(context.Background())

	require.NoError(t, err)

	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "localhost", model.Database.Host)
	assert.Equal(t, 5432, model.Database.Port)
	assert.True(t, model.Debug)
}

// stubBuilderLoader is a tiny in-memory loader used only by builder tests.
// It returns a fixed map and a recognizable source identifier so tests can
// assert which loader contributed which keys.
type stubBuilderLoader struct {
	source string
	data   map[string]any
}

func (s *stubBuilderLoader) Load(_ context.Context) (map[string]any, error) {
	return s.data, nil
}

func (s *stubBuilderLoader) Source() string { return s.source }

// chdirToTempDir switches the working directory to dir, registers a
// cleanup that restores the original working directory, and clears the
// selfconfig package cache (both immediately and after the test) so a
// fresh confii.yaml is observed.
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

// TestBuilder_AddLoader_MarksLoadersExplicit_PreventsSelfConfigAppend
// verifies G05 builder fix: a Builder caller that stages a loader via
// AddLoader must not have self-config `default_files` appended to its
// loader list. Explicit code wins over self-config.
func TestBuilder_AddLoader_MarksLoadersExplicit_PreventsSelfConfigAppend(t *testing.T) {
	dir := t.TempDir()

	// Self-config declares default_files that would, if applied, be
	// appended to the loader list. The selfconfig-supplied file places
	// `from_selfconfig: true` at the top level so the test can detect
	// it leaking through.
	confiiYAML := "default_files:\n  - selfconfig.yaml\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(confiiYAML), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "selfconfig.yaml"), []byte("from_selfconfig: true\nshared: from_selfconfig\n"), 0644))

	chdirToTempDir(t, dir)

	stub := &stubBuilderLoader{
		source: "stub://builder",
		data:   map[string]any{"from_stub": true, "shared": "from_stub"},
	}

	cfg, err := confii.NewBuilder[any]().
		AddLoader(stub).
		Build(context.Background())
	require.NoError(t, err)

	// The stub's contribution must be present.
	assert.True(t, cfg.Has("from_stub"), "expected stub loader's keys to be present")

	// Self-config's default_files MUST NOT have been appended.
	assert.False(t, cfg.Has("from_selfconfig"), "self-config default_files leaked past explicit AddLoader")

	// Where keys overlap, the stub (only loader) supplies the value.
	got, err := cfg.Get("shared")
	require.NoError(t, err)
	assert.Equal(t, "from_stub", got, "stub loader must be the only contributor when AddLoader is used")
}

// TestBuilder_NoLoaders_LeavesSelfConfigDefaultsActive verifies that a
// Builder constructed without any AddLoader call does NOT spuriously mark
// "loaders" as explicit, so self-config `default_files` are honored as
// the documented fallback (explicit > self-config > built-in default).
func TestBuilder_NoLoaders_LeavesSelfConfigDefaultsActive(t *testing.T) {
	dir := t.TempDir()

	confiiYAML := "default_files:\n  - selfconfig.yaml\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"), []byte(confiiYAML), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "selfconfig.yaml"), []byte("from_selfconfig: true\n"), 0644))

	chdirToTempDir(t, dir)

	cfg, err := confii.NewBuilder[any]().Build(context.Background())
	require.NoError(t, err)

	assert.True(t, cfg.Has("from_selfconfig"), "self-config default_files should be honored when Builder has no AddLoader call")
}
