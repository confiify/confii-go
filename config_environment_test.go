// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithEnv_BeatsEnvSwitcher_OptionsOrderForward is the end-to-end
// regression test for Validated Gap G02. Before the fix, the OS-variable
// lookup driven by WithEnvSwitcher unconditionally overwrote opts.Env even
// when the caller had explicitly chosen an environment via WithEnv,
// contradicting the documented precedence "explicit WithEnv() >
// WithEnvSwitcher() OS variable > self-config default_environment > empty
// string". This test pins the explicit-wins rule.
func TestWithEnv_BeatsEnvSwitcher_OptionsOrderForward(t *testing.T) {
	t.Setenv("CONFII_G02_APP_ENV", "staging")

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnv("production"),
		confii.WithEnvSwitcher("CONFII_G02_APP_ENV"),
	)
	require.NoError(t, err)

	assert.Equal(t, "production", cfg.Env(),
		"G02: explicit WithEnv must win over OS-variable env switcher")

	// Sanity check: the resolved env-section data should match production,
	// not staging — confirming the env name is honored downstream too.
	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)
}

// TestWithEnv_BeatsEnvSwitcher_OptionsOrderReverse exercises the same
// precedence rule but with the option list in the opposite order, proving
// the fix is order-independent. A naive guard inside WithEnvSwitcher alone
// would fail this test because WithEnvSwitcher is applied first and would
// have to be unwound retroactively when WithEnv is later applied.
func TestWithEnv_BeatsEnvSwitcher_OptionsOrderReverse(t *testing.T) {
	t.Setenv("CONFII_G02_APP_ENV", "staging")

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnvSwitcher("CONFII_G02_APP_ENV"),
		confii.WithEnv("production"),
	)
	require.NoError(t, err)

	assert.Equal(t, "production", cfg.Env(),
		"G02: explicit WithEnv must win regardless of option ordering")

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", host)
}

// TestEnvSwitcher_AppliesWithoutExplicitEnv confirms that the audit fix
// did not regress the legitimate path: when WithEnv is NOT supplied, the
// OS-variable lookup must still drive env selection.
func TestEnvSwitcher_AppliesWithoutExplicitEnv(t *testing.T) {
	t.Setenv("CONFII_G02_APP_ENV", "staging")

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/envs.yaml")),
		confii.WithEnvSwitcher("CONFII_G02_APP_ENV"),
	)
	require.NoError(t, err)

	assert.Equal(t, "staging", cfg.Env(),
		"G02: env switcher must still apply when no explicit WithEnv was used")

	host, err := cfg.Get("database.host")
	require.NoError(t, err)
	assert.Equal(t, "staging-db.example.com", host)
}
