package confii_test

import (
	"context"
	"testing"

	confii "github.com/qualitycoe/confii-go"
	"github.com/qualitycoe/confii-go/loader"
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
