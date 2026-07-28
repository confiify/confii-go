// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Wave 13 Validated Gap G03: WithEnvPrefix must add an environment loader.
//
// Before the fix, WithEnvPrefix only stored the prefix on opts.EnvPrefix
// and was consumed exclusively by the sysenv-fallback Get-time lookup
// (config.go:1992 lookupSysenv). That broke the public option contract:
// a user calling WithEnvPrefix("APP") naturally expects environment
// variables matching the prefix to participate in the standard load+merge
// pipeline alongside YAML/JSON/etc loaders, and to surface in cfg.Layers().
//
// The fix installs loader.NewEnvironment(prefix)-equivalent logic (an internal
// envPrefixAutoLoader matching the canonical "__" separator convention),
// finalizes its precedence after declarative sources, and deduplicates against
// an already-supplied loader.NewEnvironment(prefix).
// =============================================================================

// TestWithEnvPrefix_AppendsEnvironmentLoader pins the core post-fix
// contract: WithEnvPrefix("APP") plus an OS env APP_HOST=example.com
// must surface "example.com" via cfg.Get("host") through the loader
// pipeline (i.e., merged into mergedConfig), NOT only as an
// after-the-fact sysenv fallback resolution at Get-time.
//
// Pre-fix observable: the test would still pass IF WithSysenvFallback was
// also enabled (because lookupSysenv would synthesize the value), but
// here SysenvFallback is left off. With the loader missing, Get would
// return ErrNotFound. Post-fix the value must be in the merged config.
func TestWithEnvPrefix_AppendsEnvironmentLoader(t *testing.T) {
	t.Setenv("APP_HOST", "example.com")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvPrefix("APP"),
		// NOTE: SysenvFallback is intentionally NOT enabled. This forces
		// the loader pipeline to be the only path by which APP_HOST
		// can land in cfg, so the test fails pre-fix.
	)
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err,
		"G03: APP_HOST must be visible via the loader pipeline when "+
			"WithEnvPrefix is set (no sysenv-fallback enabled)")
	assert.Equal(t, "example.com", host)
}

// TestWithEnvPrefix_NestedDoubleUnderscore exercises the canonical
// "__" double-underscore nesting separator: APP_DB__HOST=postgres maps
// to db.host. This convention matches loader.EnvironmentLoader's default,
// confirming the auto-installed loader is behaviorally equivalent to a
// user-supplied loader.NewEnvironment(prefix).
func TestWithEnvPrefix_NestedDoubleUnderscore(t *testing.T) {
	t.Setenv("APP_DB__HOST", "postgres.example.com")
	t.Setenv("APP_DB__PORT", "5432")

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvPrefix("APP"),
	)
	require.NoError(t, err)

	host, err := cfg.Get("db.host")
	require.NoError(t, err)
	assert.Equal(t, "postgres.example.com", host,
		"G03: APP_DB__HOST must split on `__` and surface as db.host")

	// typecoerce.ParseScalar coerces numeric strings to int.
	port, err := cfg.Get("db.port")
	require.NoError(t, err)
	assert.EqualValues(t, 5432, port,
		"G03: APP_DB__PORT must surface as a coerced numeric value")
}

// TestWithEnvPrefix_LayersIncludesEnvironmentLoader proves the
// auto-installed loader is part of the live load pipeline (not merely a
// hidden Get-time shortcut). cfg.Layers() must list the canonical
// `environment:APP` source identifier matching
// loader.EnvironmentLoader.Source().
func TestWithEnvPrefix_LayersIncludesEnvironmentLoader(t *testing.T) {
	t.Setenv("APP_PRESENT", "yes")

	cfg, err := confii.New[any](context.Background(),
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
		"G03: cfg.Layers() must include the auto-installed env loader "+
			"under the canonical `environment:APP` source identifier; "+
			"got layers: %v", layers)
}

// TestWithEnvPrefix_NoDoubleApplyWithExplicitLoader verifies the
// dedup contract: when a user supplies their own loader.NewEnvironment("APP")
// via WithLoaders AND ALSO calls WithEnvPrefix("APP"), the env-loader
// layer must appear exactly once. Otherwise the same env vars would be
// merged twice (idempotent for scalars, but observable as a duplicated
// layer in cfg.Layers() and a doubled key-count, and incorrect for any
// future merge strategy that is order/repetition-sensitive).
//
// Detection uses the shared Source() identifier "environment:APP" so the
// auto-loader and the explicit loader collide on identity.
func TestWithEnvPrefix_NoDoubleApplyWithExplicitLoader(t *testing.T) {
	t.Setenv("APP_HOST", "example.com")

	cfg, err := confii.New[any](context.Background(),
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
		"G03: env-loader layer must appear exactly once when both "+
			"WithLoaders(loader.NewEnvironment(\"APP\")) and "+
			"WithEnvPrefix(\"APP\") are supplied; got %d", envLayerCount)

	// Sanity: value is still readable.
	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "example.com", host)
}

func TestWithEnvPrefixOptionOrderDoesNotLoseAutoLoader(t *testing.T) {
	t.Setenv("APP_HOST", "from-environment")

	cfg, err := confii.New[any](context.Background(),
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

// TestRegressionBaseline_WithoutFix_FallbackOnly is the regression
// baseline test. It pins a marker behavior that the pre-fix code path
// CANNOT produce: a key that the new env loader places into mergedConfig
// (and therefore surfaces via cfg.Layers() with a non-zero key_count for
// `environment:APP`) but that the sysenv fallback alone could not have
// found because sysenv fallback uses single-underscore key→envvar
// mapping (lookupSysenv: db.host -> DB_HOST -> APP_DB_HOST), whereas the
// loader uses double-underscore (APP_DB__HOST -> db.host).
//
// We set APP_DB__SOCKET=/tmp/g03.sock and assert:
//
//  1. The env loader layer reports key_count > 0 (post-fix only — pre-fix
//     the loader did not exist at all).
//  2. The value is readable via cfg.Get("db.socket") even with
//     SysenvFallback disabled.
//  3. Sysenv fallback for the same key (with single-underscore) would NOT
//     have found it, since APP_DB_SOCKET is unset — confirming the loader
//     is the only mechanism that can surface this value.
func TestRegressionBaseline_WithoutFix_FallbackOnly(t *testing.T) {
	t.Setenv("APP_DB__SOCKET", "/tmp/g03.sock")
	// Defensive: explicitly ensure the single-underscore variant is unset
	// so sysenv fallback (even if accidentally enabled) cannot resolve it.
	require.NoError(t, os.Unsetenv("APP_DB_SOCKET"))

	cfg, err := confii.New[any](context.Background(),
		confii.WithEnvPrefix("APP"),
		// WithSysenvFallback is intentionally OFF: pre-fix code, even
		// with fallback ON, would have used APP_DB_SOCKET (single
		// underscore) and missed the double-underscore APP_DB__SOCKET.
	)
	require.NoError(t, err)

	socket, err := cfg.Get("db.socket")
	require.NoError(t, err,
		"G03 regression: APP_DB__SOCKET must surface via the env loader "+
			"in the load+merge pipeline, not via sysenv fallback")
	assert.Equal(t, "/tmp/g03.sock", socket)

	// Verify the layer is present and reports the expected key.
	layers := cfg.Layers()
	var envLayer map[string]any
	for _, layer := range layers {
		if src, ok := layer["source"].(string); ok && src == "environment:APP" {
			envLayer = layer
			break
		}
	}
	require.NotNil(t, envLayer,
		"G03 regression: env loader layer must exist post-fix")
	keyCount, _ := envLayer["key_count"].(int)
	assert.Greater(t, keyCount, 0,
		"G03 regression: env loader layer must report >0 keys; got %d", keyCount)
}
