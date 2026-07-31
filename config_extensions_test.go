// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type extensionLoader map[string]any

func (l extensionLoader) Load(context.Context) (map[string]any, error) { return map[string]any(l), nil }
func (extensionLoader) Source() string                                 { return "extension-test" }

type textExporter struct{}

func (*textExporter) Export(data map[string]any) ([]byte, error) {
	return []byte(fmt.Sprint(data["name"])), nil
}
func (*textExporter) Format() string { return "text" }

type invalidExporter struct{ format string }

func (*invalidExporter) Export(map[string]any) ([]byte, error) { return nil, nil }
func (e *invalidExporter) Format() string                      { return e.format }

type validatorFunc func(map[string]any) error

func (fn validatorFunc) Validate(data map[string]any) error { return fn(data) }

func TestWithExporter_AddsCustomFormat(t *testing.T) {
	cfg, err := confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "confii"}),
		confii.WithExporter(&textExporter{}),
	)
	require.NoError(t, err)

	data, err := cfg.Export("text")
	require.NoError(t, err)
	assert.Equal(t, "confii", string(data))

	jsonData, err := cfg.Export("json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"confii"}`, string(jsonData))
}

func TestBuilder_WithExporterAndValidator(t *testing.T) {
	cfg, err := confii.NewBuilder[any]().
		AddLoader(extensionLoader{"name": "confii"}).
		WithExporter(&textExporter{}).
		WithValidator(validatorFunc(func(data map[string]any) error {
			if data["name"] == "" {
				return errors.New("name is required")
			}
			return nil
		})).
		Build()
	require.NoError(t, err)

	data, err := cfg.Export("text")
	require.NoError(t, err)
	assert.Equal(t, "confii", string(data))
}

func TestWithExporter_RejectsInvalidRegistration(t *testing.T) {
	var typedNil *textExporter
	for name, exporter := range map[string]confii.Exporter{
		"nil":        nil,
		"typed_nil":  typedNil,
		"empty":      &invalidExporter{},
		"uppercase":  &invalidExporter{format: "JSON"},
		"whitespace": &invalidExporter{format: " text "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := confii.New[any](confii.WithExporter(exporter))
			require.Error(t, err)
			assert.ErrorIs(t, err, confii.ErrConfigInvalid)
		})
	}
}

func TestWithValidator_RejectsCandidateAndCannotMutatePublishedData(t *testing.T) {
	mutationAttempt := validatorFunc(func(data map[string]any) error {
		data["name"] = "changed-by-validator"
		return nil
	})
	cfg, err := confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "confii"}),
		confii.WithValidator(mutationAttempt),
	)
	require.NoError(t, err)
	name, err := cfg.Get("name")
	require.NoError(t, err)
	assert.Equal(t, "confii", name)

	rejectBlocked := validatorFunc(func(data map[string]any) error {
		if data["name"] == "blocked" {
			return errors.New("name is blocked")
		}
		return nil
	})
	cfg, err = confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "confii"}),
		confii.WithValidator(rejectBlocked),
	)
	require.NoError(t, err)
	require.Error(t, cfg.Set("name", "blocked"))
	name, err = cfg.Get("name")
	require.NoError(t, err)
	assert.Equal(t, "confii", name)
}

func TestWithValidator_AppliesToEveryPublishingLifecycle(t *testing.T) {
	rejectBlocked := validatorFunc(func(data map[string]any) error {
		if data["name"] == "blocked" {
			return errors.New("name is blocked")
		}
		return nil
	})
	base := extensionLoader{"name": "confii"}
	cfg, err := confii.New[any](
		confii.WithLoaders(base),
		confii.WithValidator(rejectBlocked),
	)
	require.NoError(t, err)

	err = cfg.Extend(extensionLoader{"name": "blocked"})
	require.ErrorIs(t, err, confii.ErrConfigValidation)
	name, getErr := cfg.Get("name")
	require.NoError(t, getErr)
	assert.Equal(t, "confii", name)

	restore, err := cfg.Override(map[string]any{"name": "blocked"})
	require.ErrorIs(t, err, confii.ErrConfigValidation)
	assert.Nil(t, restore)
	name, getErr = cfg.Get("name")
	require.NoError(t, getErr)
	assert.Equal(t, "confii", name)

	base["name"] = "blocked"
	err = cfg.Reload(confii.WithIncremental(false))
	require.ErrorIs(t, err, confii.ErrConfigValidation)
	name, getErr = cfg.Get("name")
	require.NoError(t, getErr)
	assert.Equal(t, "confii", name)

	require.NoError(t, cfg.Reload(
		confii.WithIncremental(false),
		confii.WithReloadValidate(false),
	))
	name, getErr = cfg.Get("name")
	require.NoError(t, getErr)
	assert.Equal(t, "blocked", name)
}

func TestExtend_UsesCanonicalNonStrictTypedValidation(t *testing.T) {
	type appConfig struct {
		Name string `confii:"name" validate:"required"`
	}
	cfg, err := confii.New[appConfig](
		confii.WithLoaders(extensionLoader{"name": "ready"}),
		confii.WithValidateOnLoad(true),
		confii.WithStrictValidation(false),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Extend(extensionLoader{"name": ""}))
	name, err := cfg.Get("name")
	require.NoError(t, err)
	assert.Equal(t, "", name)
}

func TestWithValidator_RejectsNilAndInitialSnapshot(t *testing.T) {
	var typedNil validatorFunc
	for name, validator := range map[string]confii.Validator{
		"nil":       nil,
		"typed_nil": typedNil,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := confii.New[any](confii.WithValidator(validator))
			require.Error(t, err)
			assert.ErrorIs(t, err, confii.ErrConfigInvalid)
		})
	}

	_, err := confii.New[any](
		confii.WithLoaders(extensionLoader{"name": "blocked"}),
		confii.WithValidator(validatorFunc(func(map[string]any) error {
			return errors.New("rejected")
		})),
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigValidation)
}
