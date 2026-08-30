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

type castingProbeConfig struct {
	App struct {
		Name string `confii:"name"`
		Port int    `confii:"port"`
	} `confii:"app"`
}

func writeCastingProbeYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

const castingProbeQuotedPort = "app:\n  name: svc\n  port: \"5432\"\n"

// TestTypeCastingDisabled_TypedDecodeDoesNotConvert is the regression
// for the reported inconsistency: WithTypeCasting(false) documents
// that loaded strings are preserved verbatim, so the typed model must
// not convert them either. Before the fix the untyped snapshot kept
// "5432" while Typed reported 5432, so the same Config answered the
// same question two different ways.
func TestTypeCastingDisabled_TypedDecodeDoesNotConvert(t *testing.T) {
	path := writeCastingProbeYAML(t, castingProbeQuotedPort)

	cfg, err := confii.NewWithContext[castingProbeConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithTypeCasting(false),
	)
	require.NoError(t, err)

	value, err := cfg.Get("app.port")
	require.NoError(t, err)
	assert.Equal(t, "5432", value, "the snapshot must preserve the loaded string")

	_, err = cfg.TypedCopy()
	require.Error(t, err, "the typed decode must not convert what casting preserved")
	assert.Contains(t, err.Error(), "struct decode")
}

// TestTypeCastingEnabled_TypedDecodeConverts pins the default: with
// casting enabled the snapshot and the typed model agree on the
// converted value.
func TestTypeCastingEnabled_TypedDecodeConverts(t *testing.T) {
	path := writeCastingProbeYAML(t, castingProbeQuotedPort)

	cfg, err := confii.NewWithContext[castingProbeConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithTypeCasting(true),
	)
	require.NoError(t, err)

	value, err := cfg.Get("app.port")
	require.NoError(t, err)
	assert.Equal(t, 5432, value)

	model, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, 5432, model.App.Port)
}

// TestTypeCastingDisabled_ExactTypesStillDecode proves disabling
// conversion does not disable decoding: input that already carries the
// declared types materializes normally.
func TestTypeCastingDisabled_ExactTypesStillDecode(t *testing.T) {
	path := writeCastingProbeYAML(t, "app:\n  name: svc\n  port: 5432\n")

	cfg, err := confii.NewWithContext[castingProbeConfig](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithTypeCasting(false),
	)
	require.NoError(t, err)

	model, err := cfg.TypedCopy()
	require.NoError(t, err)
	assert.Equal(t, 5432, model.App.Port)
	assert.Equal(t, "svc", model.App.Name)
}
