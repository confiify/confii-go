package confii

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigError_Unwrap(t *testing.T) {
	err := NewLoadError("config.yaml", errors.New("file broken"))
	assert.True(t, errors.Is(err, ErrConfigLoad))

	var ce *ConfigError
	assert.True(t, errors.As(err, &ce))
	assert.Equal(t, "Load", ce.Op)
	assert.Equal(t, "config.yaml", ce.Source)
}

func TestConfigError_ErrorMessage(t *testing.T) {
	err := NewNotFoundError("database.host", []string{"database.port", "debug"})
	msg := err.Error()
	assert.Contains(t, msg, "database.host")
	assert.Contains(t, msg, "config key not found")
}

func TestNewFormatError(t *testing.T) {
	err := NewFormatError("bad.yaml", "yaml", errors.New("parse fail"))
	assert.True(t, errors.Is(err, ErrConfigFormat))
	assert.Contains(t, err.Error(), "yaml")
}

func TestNewFrozenError(t *testing.T) {
	err := NewFrozenError("Set")
	assert.True(t, errors.Is(err, ErrConfigFrozen))
	assert.Contains(t, err.Error(), "Set")
}
