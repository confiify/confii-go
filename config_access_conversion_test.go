// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeys_ReturnsFullPrefixedKeys(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("db.host", "localhost"))
	require.NoError(t, cfg.Set("db.port", 5432))
	require.NoError(t, cfg.Set("redis.host", "rh"))

	keys := cfg.Keys("db.")
	sort.Strings(keys)
	assert.Equal(t, []string{"db.host", "db.port"}, keys,
		"Keys(prefix) must return fully qualified keys")

	keysNoDot := cfg.Keys("db")
	sort.Strings(keysNoDot)
	assert.Equal(t, keys, keysNoDot,
		`Keys("db") and Keys("db.") must return the same set`)

	for _, k := range keys {
		v, err := cfg.Get(k)
		require.NoError(t, err, "Get(%q) must succeed for a key returned by Keys", k)
		assert.NotNil(t, v)
	}

	for _, k := range keys {
		assert.False(t, k == "redis.host",
			"Keys(prefix) must filter out non-matching keys")
	}
}

func TestHas_HonorsSysenvFallback(t *testing.T) {
	t.Setenv("APP_DB_PASSWORD", "secret")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("APP"),
		confii.WithSysenvFallback(true),
	)
	require.NoError(t, err)

	v, err := cfg.Get("db.password")
	require.NoError(t, err)
	assert.Equal(t, "secret", v)

	assert.True(t, cfg.Has("db.password"),
		"Has must return true when system-environment fallback resolves the key")

	assert.False(t, cfg.Has("db.nonexistent.key"),
		"Has must return false when neither config nor sysenv has the key")
}

func TestHas_NoSysenvFallback_StillRespectsOption(t *testing.T) {
	t.Setenv("APP_DB_PASSWORD", "secret")

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithEnvPrefix("APP"),
	)
	require.NoError(t, err)

	assert.False(t, cfg.Has("db.password"),
		"Has must NOT consult sysenv when SysenvFallback is disabled")
}

func TestGetInt_RejectsNonIntegerFloat(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("port", 3.14))

	v, err := cfg.GetInt("port")
	require.Error(t, err, "GetInt must reject a non-integer float")
	assert.Equal(t, 0, v, "rejected GetInt must return zero value")

	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"GetInt rejection must wrap ErrConfigInvalid")
	assert.Equal(t, "GetInt", ce.Op)
	assert.Equal(t, "port", ce.Key)
}

func TestGetInt_AcceptsIntegerFloat(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("port", 3.0))
	v, err := cfg.GetInt("port")
	require.NoError(t, err, "GetInt must accept float with zero fractional part")
	assert.Equal(t, 3, v)

	require.NoError(t, cfg.Set("offset", -7.0))
	v, err = cfg.GetInt("offset")
	require.NoError(t, err)
	assert.Equal(t, -7, v)
}

func TestGetInt_RejectsNonFiniteFloat(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)

	require.NoError(t, cfg.Set("nan", 0.0/zeroFloat()))
	_, err = cfg.GetInt("nan")
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigInvalid),
		"NaN must be rejected with ErrConfigInvalid")
}

func zeroFloat() float64 {
	z := 0.0
	return z
}

func TestGetBool_AcceptsAllDocumentedForms(t *testing.T) {
	cases := []struct {
		raw  any
		want bool
	}{

		{true, true},
		{false, false},

		{"true", true},
		{"True", true},
		{"TRUE", true},
		{" true ", true},
		{"false", false},
		{"False", false},

		{"1", true},
		{"0", false},

		{"yes", true},
		{"YES", true},
		{"no", false},

		{"on", true},
		{"off", false},

		{1, true},
		{0, false},
		{int64(1), true},
		{int64(0), false},
		{1.0, true},
		{0.0, false},
	}

	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			cfg, err := confii.NewWithContext[any](context.Background())
			require.NoError(t, err)
			require.NoError(t, cfg.Set("flag", tc.raw))
			got, err := cfg.GetBool("flag")
			require.NoError(t, err, "GetBool must accept %v (%T)", tc.raw, tc.raw)
			assert.Equal(t, tc.want, got, "GetBool(%v) = %v, want %v", tc.raw, got, tc.want)
		})
	}
}

func TestGetBool_RejectsUnknownString(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background())
	require.NoError(t, err)
	require.NoError(t, cfg.Set("flag", "maybe"))

	_, err = cfg.GetBool("flag")
	require.Error(t, err)
	var ce *confii.ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.Equal(t, "GetBool", ce.Op)
	assert.Equal(t, "flag", ce.Key)
}
