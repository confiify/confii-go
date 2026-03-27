package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestConfig struct {
	Database DatabaseConfig `mapstructure:"database"`
	Debug    bool           `mapstructure:"debug"`
}

type DatabaseConfig struct {
	Host string `mapstructure:"host" validate:"required"`
	Port int    `mapstructure:"port" validate:"required,min=1,max=65535"`
	Name string `mapstructure:"name" validate:"required"`
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
			// port and name missing
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
			"port": 0, // invalid: min=1
			"name": "mydb",
		},
	}

	_, err := DecodeAndValidate[TestConfig](data)
	assert.Error(t, err)
}
