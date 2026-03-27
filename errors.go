package confii

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for use with errors.Is.
var (
	ErrConfigLoad       = errors.New("config load error")
	ErrConfigFormat     = errors.New("config format error")
	ErrConfigValidation = errors.New("config validation error")
	ErrConfigNotFound   = errors.New("config key not found")
	ErrConfigMerge      = errors.New("config merge conflict")
	ErrConfigFrozen     = errors.New("config is frozen")
	ErrConfigAccess     = errors.New("config access error")
	ErrSecretNotFound   = errors.New("secret not found")
	ErrSecretAccess     = errors.New("secret access error")
	ErrSecretStore      = errors.New("secret store error")
	ErrSecretValidation = errors.New("secret validation error")
	ErrVaultAuth        = errors.New("vault authentication error")
)

// ConfigError is a structured error with operation context.
type ConfigError struct {
	Op      string // operation that failed (e.g., "Load", "Get", "Set")
	Source  string // source identifier (e.g., file path, URL)
	Key     string // config key involved, if applicable
	Err     error  // underlying error (wraps a sentinel)
	Context map[string]any
}

func (e *ConfigError) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Source != "" {
		fmt.Fprintf(&b, "[%s] ", e.Source)
	}
	if e.Key != "" {
		fmt.Fprintf(&b, "key %q: ", e.Key)
	}
	if e.Err != nil {
		b.WriteString(e.Err.Error())
	}
	if len(e.Context) > 0 {
		fmt.Fprintf(&b, " (%v)", e.Context)
	}
	return b.String()
}

func (e *ConfigError) Unwrap() error {
	return e.Err
}

// NewLoadError creates a config load error.
func NewLoadError(source string, err error) error {
	return &ConfigError{
		Op:     "Load",
		Source: source,
		Err:    fmt.Errorf("%w: %v", ErrConfigLoad, err),
	}
}

// NewFormatError creates a config format/parse error.
func NewFormatError(source, formatType string, err error) error {
	return &ConfigError{
		Op:     "Parse",
		Source: source,
		Err:    fmt.Errorf("%w: %v", ErrConfigFormat, err),
		Context: map[string]any{
			"format_type": formatType,
		},
	}
}

// NewNotFoundError creates a key-not-found error with available keys.
func NewNotFoundError(key string, availableKeys []string) error {
	return &ConfigError{
		Op:  "Get",
		Key: key,
		Err: ErrConfigNotFound,
		Context: map[string]any{
			"available_keys": availableKeys,
		},
	}
}

// NewValidationError creates a config validation error.
func NewValidationError(errs []string, original error) error {
	return &ConfigError{
		Op:  "Validate",
		Err: fmt.Errorf("%w: %v", ErrConfigValidation, original),
		Context: map[string]any{
			"validation_errors": errs,
		},
	}
}

// NewFrozenError creates an error for operations on frozen config.
func NewFrozenError(op string) error {
	return &ConfigError{
		Op:  op,
		Err: ErrConfigFrozen,
	}
}
