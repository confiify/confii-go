// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"testing"
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSet_RollbackFidelity_TypePreservation_Int(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("port", 8080))

	pre, err := cfg.Get("port")
	require.NoError(t, err)
	require.IsType(t, int(0), pre,
		"precondition: Set must store int as int (no Flatten/Unflatten on the success path)")

	err = cfg.Set("port.subkey", "x")
	require.Error(t, err, "Set through a scalar must error ")

	post, err := cfg.Get("port")
	require.NoError(t, err,
		"after rollback the original int leaf must still be queryable")
	assert.IsType(t, int(0), post,
		"F-Set-RollbackFidelity: rollback must preserve int type (not collapse to float64)")
	assert.Equal(t, 8080, post,
		"F-Set-RollbackFidelity: rollback must preserve int value")
}

func TestSet_RollbackFidelity_TypePreservation_Duration(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	want := 5 * time.Second
	require.NoError(t, cfg.Set("timeout", want))

	pre, err := cfg.Get("timeout")
	require.NoError(t, err)
	require.IsType(t, time.Duration(0), pre,
		"precondition: time.Duration must round-trip as Duration")

	err = cfg.Set("timeout.subkey", "x")
	require.Error(t, err)

	post, err := cfg.Get("timeout")
	require.NoError(t, err,
		"after rollback the original Duration leaf must still be queryable")
	assert.IsType(t, time.Duration(0), post,
		"F-Set-RollbackFidelity: rollback must preserve time.Duration type (not collapse to int64)")
	assert.Equal(t, want, post)
}

func TestSet_RollbackFidelity_NilLeafDistinction(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("explicit_nil", nil))
	require.True(t, cfg.Has("explicit_nil"),
		"precondition: Set(key, nil) must register the key as present")

	require.NoError(t, cfg.Set("port", 8080))
	err = cfg.Set("port.subkey", "x")
	require.Error(t, err)

	assert.True(t, cfg.Has("explicit_nil"),
		"F-Set-RollbackFidelity: rollback must preserve nil-leaf distinction (key with explicit nil != missing key)")
}

func TestSet_RollbackFidelity_SubTreePreservation(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	subTree := map[string]any{
		"host": "primary.example.com",
		"port": 9090,
		"tls": map[string]any{
			"enabled":  true,
			"min_ver":  "1.3",
			"min_port": 4433,
		},
		"plugins": map[string]any{},
	}
	require.NoError(t, cfg.Set("service", subTree))
	require.True(t, cfg.Has("service.plugins"),
		"precondition: explicit empty sub-map must be reachable via Has")

	require.NoError(t, cfg.Set("port", 8080))
	err = cfg.Set("port.subkey", "x")
	require.Error(t, err)

	host, err := cfg.GetString("service.host")
	require.NoError(t, err)
	assert.Equal(t, "primary.example.com", host)

	port, err := cfg.Get("service.port")
	require.NoError(t, err)
	assert.IsType(t, int(0), port,
		"sub-tree leaf int must remain int after rollback")
	assert.Equal(t, 9090, port)

	tlsEnabled, err := cfg.GetBool("service.tls.enabled")
	require.NoError(t, err)
	assert.True(t, tlsEnabled)

	tlsMinPort, err := cfg.Get("service.tls.min_port")
	require.NoError(t, err)
	assert.IsType(t, int(0), tlsMinPort,
		"deeply nested sub-tree int must remain int after rollback")
	assert.Equal(t, 4433, tlsMinPort)

	assert.True(t, cfg.Has("service.plugins"),
		"an explicit empty sub-map must survive rollback")
}

func TestSet_RollbackFidelity_EmptySubMapSurvivesActiveRollback(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("plugins", map[string]any{}))
	require.True(t, cfg.Has("plugins"),
		"precondition: empty sub-map must be Has-reachable")

	err = cfg.Set("debug.feature", true)
	require.Error(t, err)

	assert.True(t, cfg.Has("plugins"),
		"rollback after setting an empty sub-map must preserve the empty sub-map")
}

func TestSet_RollbackFidelity_FirstSetNestedFailure_NoIntermediates(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	preKeys := map[string]struct{}{}
	for _, k := range cfg.Keys() {
		preKeys[k] = struct{}{}
	}

	err = cfg.Set("debug.feature.flag.deep.path", true)
	require.Error(t, err)

	postKeys := map[string]struct{}{}
	for _, k := range cfg.Keys() {
		postKeys[k] = struct{}{}
	}

	for k := range postKeys {
		_, ok := preKeys[k]
		assert.True(t, ok,
			"F-Set-RollbackFidelity: failed Set must not leak key %q (rollback regressed)", k)
	}
	for k := range preKeys {
		_, ok := postKeys[k]
		assert.True(t, ok,
			"F-Set-RollbackFidelity: failed Set must not delete pre-existing key %q", k)
	}
}

func TestSet_RollbackFidelity_TrackerNoStaleRuntimeEntry(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	err = cfg.Set("debug.feature.flag", true)
	require.Error(t, err)

	assert.False(t, cfg.Has("debug.feature.flag"),
		"failed Set must not leave the rejected key reachable")

	info := cfg.Explain("debug.feature.flag")
	if info["exists"] == true {
		assert.NotEqual(t, "runtime", info["source"],
			"F-Set-RollbackFidelity: failed Set must not leave a stale 'runtime' tracker entry")
	}
}

func TestSet_RollbackFidelity_EnvAndMergedRemainInLockstep(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	preDict, err := cfg.ToDict()
	require.NoError(t, err)

	err = cfg.Set("debug.x.y", true)
	require.Error(t, err)

	postDict, dictErr := cfg.ToDict()
	require.NoError(t, dictErr)

	assert.Equal(t, preDict, postDict,
		"F-Set-RollbackFidelity: failed Set must leave envConfig and mergedConfig in lockstep with their pre-Set snapshots")
}
