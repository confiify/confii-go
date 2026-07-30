// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestConfig struct {
	Database DatabaseConfig `confii:"database"`
	Debug    bool           `confii:"debug"`
}

type DatabaseConfig struct {
	Host string `confii:"host" validate:"required"`
	Port int    `confii:"port" validate:"required,min=1,max=65535"`
	Name string `confii:"name" validate:"required"`
}

func TestStructValidator_Validate(t *testing.T) {
	v := NewStructValidator[TestConfig]()

	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
			"name": "mydb",
		},
		"debug": true,
	}
	err := v.Validate(data)
	assert.NoError(t, err)
}

func TestStructValidator_Validate_MissingRequired(t *testing.T) {
	v := NewStructValidator[TestConfig]()

	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
		},
		"debug": true,
	}
	err := v.Validate(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}

func TestDecode(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
			"name": "mydb",
		},
		"debug": true,
	}

	result, err := Decode[TestConfig](data)
	require.NoError(t, err)
	assert.Equal(t, "localhost", result.Database.Host)
	assert.Equal(t, 5432, result.Database.Port)
	assert.True(t, result.Debug)
}

func TestDecodeAndValidate(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 5432,
			"name": "mydb",
		},
	}

	result, err := DecodeAndValidate[TestConfig](data)
	require.NoError(t, err)
	assert.Equal(t, "localhost", result.Database.Host)
}

func TestDecodeAndValidate_Invalid(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 0,
			"name": "mydb",
		},
	}

	_, err := DecodeAndValidate[TestConfig](data)
	assert.Error(t, err)
}

type ConfiiTagTLS struct {
	TLS bool `confii:"tls"`
}

type confiiTagEndpoint struct {
	Host string `confii:"host"`
	Port int    `confii:"port"`
}

type confiiTagConfig struct {
	DisplayName  string `confii:"display_name"`
	Ignored      string `confii:"-"`
	ConfiiTagTLS `confii:",squash"`
	Primary      *confiiTagEndpoint           `confii:"primary"`
	Replicas     []confiiTagEndpoint          `confii:"replicas"`
	Named        map[string]confiiTagEndpoint `confii:"named"`
	Extra        map[string]any               `confii:",remain"`
}

func TestDecode_ConfiiTagContract(t *testing.T) {
	result, err := Decode[confiiTagConfig](map[string]any{
		"display_name": "payments",
		"ignored":      "must-not-bind",
		"tls":          "true",
		"primary": map[string]any{
			"host": "primary.internal",
			"port": "5432",
		},
		"replicas": []any{
			map[string]any{"host": "replica.internal", "port": 5433},
		},
		"named": map[string]any{
			"analytics": map[string]any{"host": "analytics.internal", "port": 5434},
		},
		"region": "ap-south-1",
	})
	require.NoError(t, err)

	assert.Equal(t, "payments", result.DisplayName)
	assert.Empty(t, result.Ignored)
	assert.True(t, result.TLS)
	require.NotNil(t, result.Primary)
	assert.Equal(t, "primary.internal", result.Primary.Host)
	assert.Equal(t, 5432, result.Primary.Port)
	require.Len(t, result.Replicas, 1)
	assert.Equal(t, "replica.internal", result.Replicas[0].Host)
	assert.Equal(t, 5434, result.Named["analytics"].Port)
	assert.Equal(t, "ap-south-1", result.Extra["region"])
	assert.Equal(t, "must-not-bind", result.Extra["ignored"])
}

func TestDecode_MapstructureTagIsNotAPublicAlias(t *testing.T) {
	type legacyTagged struct {
		Name string `mapstructure:"legacy_name"`
	}

	result, err := Decode[legacyTagged](map[string]any{"legacy_name": "legacy"})
	require.NoError(t, err)
	assert.Empty(t, result.Name)
}
