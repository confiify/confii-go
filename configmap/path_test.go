// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package configmap_test

import (
	"errors"
	"testing"

	"github.com/confiify/confii-go/v2/configmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAndHas(t *testing.T) {
	data := map[string]any{
		"database": map[string]any{"host": "localhost", "password": nil},
		"debug":    true,
	}

	value, found := configmap.Get(data, "database.host")
	assert.True(t, found)
	assert.Equal(t, "localhost", value)
	assert.True(t, configmap.Has(data, "database.password"))
	assert.False(t, configmap.Has(data, "database.port"))
	assert.False(t, configmap.Has(data, "database.host.name"))
	assert.False(t, configmap.Has(data, "database..host"))
	assert.False(t, configmap.Has(nil, "database.host"))
}

func TestSetCreatesAndReplacesValues(t *testing.T) {
	data := map[string]any{"optional": map[string]any(nil)}
	require.NoError(t, configmap.Set(data, "database.primary.host", "localhost"))
	require.NoError(t, configmap.Set(data, "database.primary.port", 5432))
	require.NoError(t, configmap.Set(data, "debug", true))
	require.NoError(t, configmap.Set(data, "debug", false))
	require.NoError(t, configmap.Set(data, "optional.enabled", true))

	assert.Equal(t, map[string]any{
		"database": map[string]any{
			"primary": map[string]any{"host": "localhost", "port": 5432},
		},
		"debug":    false,
		"optional": map[string]any{"enabled": true},
	}, data)
}

func TestSetReturnsTypedErrorsWithoutPartialMutation(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		path string
		want error
	}{
		{name: "empty", data: map[string]any{"keep": true}, path: "", want: configmap.ErrInvalidPath},
		{name: "empty segment", data: map[string]any{"keep": true}, path: "database..host", want: configmap.ErrInvalidPath},
		{name: "nil map", data: nil, path: "database.host", want: configmap.ErrNilMap},
		{name: "scalar conflict", data: map[string]any{"database": "dsn"}, path: "database.host", want: configmap.ErrPathConflict},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := clone(test.data)
			err := configmap.Set(test.data, test.path, "value")
			require.Error(t, err)
			assert.ErrorIs(t, err, test.want)
			var pathErr *configmap.PathError
			assert.ErrorAs(t, err, &pathErr)
			assert.Equal(t, before, test.data)
		})
	}
}

func TestKeysAreSortedQualifiedFilteredAndCycleSafe(t *testing.T) {
	shared := map[string]any{"host": "localhost"}
	data := map[string]any{
		"z":        true,
		"database": map[string]any{"port": 5432, "primary": shared},
		"mirror":   shared,
		"empty":    map[string]any{},
	}
	data["cycle"] = data

	assert.Equal(t, []string{
		"database.port",
		"database.primary.host",
		"mirror.host",
		"z",
	}, configmap.Keys(data))
	assert.Equal(t, []string{
		"database.port",
		"database.primary.host",
	}, configmap.Keys(data, "database"))
	assert.Equal(t, configmap.Keys(data, "database"), configmap.Keys(data, "database."))
	assert.Empty(t, configmap.Keys(data, "database..primary"))
	assert.Empty(t, configmap.Keys(nil))
}

func TestPathErrorNilReceiver(t *testing.T) {
	var pathErr *configmap.PathError
	assert.Equal(t, "<nil>", pathErr.Error())
	assert.NoError(t, pathErr.Unwrap())
	assert.False(t, errors.Is(pathErr, configmap.ErrInvalidPath))
}

func clone(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	result := make(map[string]any, len(data))
	for key, value := range data {
		result[key] = value
	}
	return result
}
