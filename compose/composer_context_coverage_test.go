// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package compose

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComposeWithDependenciesContextAdmission(t *testing.T) {
	composer := New(t.TempDir())
	result, dependencies, err := composer.ComposeWithDependencies(map[string]any{"key": "value"}, "config.yaml")
	require.NoError(t, err)
	require.Equal(t, "value", result["key"])
	require.Empty(t, dependencies)

	var nilContext context.Context
	_, _, err = composer.ComposeWithDependenciesWithContext(nilContext, nil, "config.yaml")
	require.Error(t, err)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = composer.ComposeWithDependenciesWithContext(canceled, nil, "config.yaml")
	require.ErrorIs(t, err, context.Canceled)
}

func TestComposeCancellationBeforeIncludeIO(t *testing.T) {
	dir := t.TempDir()
	include := filepath.Join(dir, "include.yaml")
	require.NoError(t, os.WriteFile(include, []byte("key: value\n"), 0o600))

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	composer := New(dir)
	_, err := composer.processIncludes(canceled, "include.yaml", filepath.Join(dir, "config.yaml"), 0, map[string]bool{}, new([]string))
	require.ErrorIs(t, err, context.Canceled)
	_, err = composer.loadFile(canceled, include, 0, map[string]bool{}, new([]string))
	require.ErrorIs(t, err, context.Canceled)
}
