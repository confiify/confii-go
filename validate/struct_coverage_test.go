package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ServerConfig struct {
	Host     string `mapstructure:"host" validate:"required,hostname|ip"`
	Port     int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Protocol string `mapstructure:"protocol" validate:"oneof=http https"`
}

type AppConfig struct {
	Name    string       `mapstructure:"name" validate:"required,min=1,max=100"`
	Version string       `mapstructure:"version" validate:"required"`
	Server  ServerConfig `mapstructure:"server"`
	Debug   bool         `mapstructure:"debug"`
}

func TestStructValidator_ValidateNested(t *testing.T) {
	v := NewStructValidator[AppConfig]()

	data := map[string]any{
		"name":    "myapp",
		"version": "1.0.0",
		"server": map[string]any{
			"host":     "localhost",
			"port":     8080,
			"protocol": "https",
		},
		"debug": false,
	}
	err := v.Validate(data)
	assert.NoError(t, err)
}

func TestStructValidator_ValidateNestedMissingRequired(t *testing.T) {
	v := NewStructValidator[AppConfig]()

	data := map[string]any{
		"name":    "myapp",
		"version": "1.0.0",
		"server": map[string]any{
			"host": "localhost",
			// port and protocol missing
		},
	}
	err := v.Validate(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}

func TestStructValidator_ValidateInvalidEnum(t *testing.T) {
	v := NewStructValidator[AppConfig]()

	data := map[string]any{
		"name":    "myapp",
		"version": "1.0.0",
		"server": map[string]any{
			"host":     "localhost",
			"port":     8080,
			"protocol": "ftp", // not in oneof
		},
	}
	err := v.Validate(data)
	assert.Error(t, err)
}

func TestStructValidator_ValidatePortRange(t *testing.T) {
	v := NewStructValidator[AppConfig]()

	data := map[string]any{
		"name":    "myapp",
		"version": "1.0.0",
		"server": map[string]any{
			"host":     "localhost",
			"port":     70000, // exceeds max
			"protocol": "http",
		},
	}
	err := v.Validate(data)
	assert.Error(t, err)
}

func TestDecode_EmptyMap(t *testing.T) {
	result, err := Decode[TestConfig](map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "", result.Database.Host)
	assert.Equal(t, 0, result.Database.Port)
}

func TestDecode_ExtraFields(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host":  "localhost",
			"port":  5432,
			"name":  "mydb",
			"extra": "ignored",
		},
		"unknown_field": "value",
	}
	result, err := Decode[TestConfig](data)
	require.NoError(t, err)
	assert.Equal(t, "localhost", result.Database.Host)
}

func TestDecode_WeaklyTypedInput(t *testing.T) {
	// mapstructure's WeaklyTypedInput should handle string-to-int coercion.
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": "5432",
			"name": "mydb",
		},
		"debug": "true",
	}
	result, err := Decode[TestConfig](data)
	require.NoError(t, err)
	assert.Equal(t, 5432, result.Database.Port)
	assert.True(t, result.Debug)
}

func TestDecodeAndValidate_AllFieldsValid(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "prod-db.example.com",
			"port": 3306,
			"name": "production",
		},
		"debug": false,
	}
	result, err := DecodeAndValidate[TestConfig](data)
	require.NoError(t, err)
	assert.Equal(t, "prod-db.example.com", result.Database.Host)
	assert.Equal(t, 3306, result.Database.Port)
	assert.False(t, result.Debug)
}

func TestDecodeAndValidate_MissingAllRequired(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{},
	}
	_, err := DecodeAndValidate[TestConfig](data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}

func TestDecodeAndValidate_PortZero(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": 0,
			"name": "db",
		},
	}
	_, err := DecodeAndValidate[TestConfig](data)
	assert.Error(t, err)
}

func TestDecodeAndValidate_PortNegative(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{
			"host": "localhost",
			"port": -1,
			"name": "db",
		},
	}
	_, err := DecodeAndValidate[TestConfig](data)
	assert.Error(t, err)
}

type MinimalConfig struct {
	Name string `mapstructure:"name" validate:"required"`
}

func TestDecodeAndValidate_MinimalStruct(t *testing.T) {
	result, err := DecodeAndValidate[MinimalConfig](map[string]any{"name": "test"})
	require.NoError(t, err)
	assert.Equal(t, "test", result.Name)

	_, err = DecodeAndValidate[MinimalConfig](map[string]any{})
	assert.Error(t, err)
}

type EmailConfig struct {
	Email string `mapstructure:"email" validate:"required,email"`
	URL   string `mapstructure:"url" validate:"omitempty,url"`
}

func TestStructValidator_ValidateEmailAndURL(t *testing.T) {
	v := NewStructValidator[EmailConfig]()

	assert.NoError(t, v.Validate(map[string]any{
		"email": "user@example.com",
		"url":   "https://example.com",
	}))

	assert.Error(t, v.Validate(map[string]any{
		"email": "not-an-email",
	}))

	// Empty URL is fine (omitempty).
	assert.NoError(t, v.Validate(map[string]any{
		"email": "user@example.com",
	}))
}
