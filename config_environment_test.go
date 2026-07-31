// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithEnv_BeatsEnvSwitcher_OptionsOrderForward(t *testing.T) {
	t.Setenv("CONFII_G02_APP_ENV", "staging")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnv("production"),
		confii.WithEnvSwitcher("CONFII_G02_APP_ENV"),
	)
	require.NoError(t, err)

	assert.Equal(t, "production", cfg.Env(),
		":explicit WithEnv must win over OS-variable env switcher")

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)
}

func TestWithEnv_BeatsEnvSwitcher_OptionsOrderReverse(t *testing.T) {
	t.Setenv("CONFII_G02_APP_ENV", "staging")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnvSwitcher("CONFII_G02_APP_ENV"),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	assert.Equal(t, "production", cfg.Env(),
		":explicit WithEnv must win regardless of option ordering")

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)
}

func TestEnvSwitcher_AppliesWithoutExplicitEnv(t *testing.T) {
	t.Setenv("CONFII_G02_APP_ENV", "staging")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnvSwitcher("CONFII_G02_APP_ENV"),
	)
	require.NoError(t, err)

	assert.Equal(t, "staging", cfg.Env(),
		":env switcher must still apply when no explicit WithEnv was used")

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "staging-db.example.com", host)
}
