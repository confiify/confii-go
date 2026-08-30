// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
)

type unknownKeyConfig struct {
	App struct {
		Name string `confii:"name"`
		Port int    `confii:"port"`
	} `confii:"app"`
}

func writeUnknownKeyYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// A one-letter typo: "prot" is not a declared field.
const unknownKeyTypoYAML = "app:\n  name: svc\n  prot: 8080\n"

// TestRejectUnknownKeysDisabled_TypoIsSilent documents the default:
// an undeclared key leaves the field at its zero value with no error,
// which is indistinguishable from an intentionally absent setting.
func TestRejectUnknownKeysDisabled_TypoIsSilent(t *testing.T) {
	path := writeUnknownKeyYAML(t, unknownKeyTypoYAML)

	cfg, err := confii.NewWithContext[unknownKeyConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
	)
	require.NoError(t, err)

	model, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, "svc", model.App.Name)
	assert.Zero(t, model.App.Port, "the typo is silently unused by default")
}

// TestRejectUnknownKeysEnabled_TypoFails proves the option turns the
// same typo into a named failure.
func TestRejectUnknownKeysEnabled_TypoFails(t *testing.T) {
	path := writeUnknownKeyYAML(t, unknownKeyTypoYAML)

	cfg, err := confii.NewWithContext[unknownKeyConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithRejectUnknownKeys(true),
	)
	require.NoError(t, err)

	_, err = cfg.TypedCopy()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prot")
}

// TestRejectUnknownKeysEnabled_ValidateOnLoadFailsConstruction proves
// the policy also applies to the load-time validation path, so a
// mistyped key can be caught at construction rather than first read.
func TestRejectUnknownKeysEnabled_ValidateOnLoadFailsConstruction(t *testing.T) {
	path := writeUnknownKeyYAML(t, unknownKeyTypoYAML)

	_, err := confii.NewWithContext[unknownKeyConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithRejectUnknownKeys(true),
		confii.WithValidateOnLoad(true),
		confii.WithStrictValidation(true),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prot")
}

// TestRejectUnknownKeysEnabled_DeclaredConfigStillLoads proves only
// undeclared keys are rejected.
func TestRejectUnknownKeysEnabled_DeclaredConfigStillLoads(t *testing.T) {
	path := writeUnknownKeyYAML(t, "app:\n  name: svc\n  port: 8080\n")

	cfg, err := confii.NewWithContext[unknownKeyConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithRejectUnknownKeys(true),
		confii.WithValidateOnLoad(true),
	)
	require.NoError(t, err)

	model, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, 8080, model.App.Port)
}

// TestRejectUnknownKeysEnabled_UntypedConfigUnaffected proves
// Config[any] keeps working: without declared fields there is nothing
// for a key to be unused against.
func TestRejectUnknownKeysEnabled_UntypedConfigUnaffected(t *testing.T) {
	path := writeUnknownKeyYAML(t, unknownKeyTypoYAML)

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithRejectUnknownKeys(true),
	)
	require.NoError(t, err)

	value, err := cfg.Get("app.prot")
	require.NoError(t, err)
	assert.Equal(t, 8080, value)
}
