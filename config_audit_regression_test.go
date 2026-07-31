// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// auditMapLoader is a minimal in-memory Loader for lifecycle regression tests.
type auditMapLoader struct {
	src  string
	data map[string]any
}

type auditFailingExporter struct {
	err error
}

func (e auditFailingExporter) Export(map[string]any) ([]byte, error) { return nil, e.err }
func (auditFailingExporter) Format() string                          { return "audit_failure" }

func (l auditMapLoader) Load(context.Context) (map[string]any, error) {
	return dictutil.DeepCopy(l.data), nil
}

func (l auditMapLoader) Source() string { return l.src }

// A closed Config is a final immutable snapshot: an outstanding override
// restore function invoked after Close must not mutate published state.
func TestOverrideRestoreAfterClose_NoOp(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	restore, err := cfg.Override(map[string]any{"audit.key": "held"})
	require.NoError(t, err)

	val, err := cfg.Get("audit.key")
	require.NoError(t, err)
	require.Equal(t, "held", val)

	require.NoError(t, cfg.Close())
	restore()

	val, err = cfg.Get("audit.key")
	require.NoError(t, err,
		"restore after Close must leave the final snapshot readable and unchanged")
	assert.Equal(t, "held", val,
		"restore after Close must not republish pre-override state")
}

// A rejected overwrite must surface through the structured error contract,
// not as an anonymous message-only error.
func TestSetWithOverrideFalse_StructuredError(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)
	defer func() { _ = cfg.Close() }()

	require.NoError(t, cfg.Set("audit.existing", 1))

	err = cfg.Set("audit.existing", 2, confii.WithOverride(false))
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigInvalid)

	var configErr *confii.ConfigError
	require.ErrorAs(t, err, &configErr)
	assert.Equal(t, confii.ConfigErrorCodeInvalid, configErr.Code)
	assert.Equal(t, "audit.existing", configErr.Key)
}

// Rollback admission failures must use stable error categories.
func TestRollback_StructuredErrors(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)
	defer func() { _ = cfg.Close() }()

	//nolint:staticcheck // the nil-context admission branch is the contract under test.
	err = cfg.RollbackToVersionWithContext(nil, "any")
	assert.ErrorIs(t, err, confii.ErrConfigInvalid, "nil context")

	//nolint:staticcheck // the nil-context admission branch is the contract under test.
	_, err = cfg.SaveVersionWithContext(nil, nil)
	assert.ErrorIs(t, err, confii.ErrConfigInvalid, "SaveVersion nil context")

	err = cfg.RollbackToVersion("any")
	assert.ErrorIs(t, err, confii.ErrConfigInvalid, "versioning not enabled")

	require.NotNil(t, cfg.EnableVersioning("", 0))
	err = cfg.RollbackToVersion("missing-version")
	assert.ErrorIs(t, err, confii.ErrConfigNotFound, "unknown version id")

	var configErr *confii.ConfigError
	require.ErrorAs(t, err, &configErr)
	assert.Equal(t, "missing-version", configErr.Key)
}

// Operation-specific event payloads must be detached: a listener mutating a
// slice value inside the payload must not change published configuration.
func TestExtendEventPayload_DetachedSlices(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)
	defer func() { _ = cfg.Close() }()

	fired := false
	cfg.EnableEvents().On("extend", func(args ...any) {
		payload, ok := args[0].(map[string]any)
		if !ok {
			return
		}
		section, ok := payload["audit"].(map[string]any)
		if !ok {
			return
		}
		items, ok := section["items"].([]any)
		if !ok || len(items) == 0 {
			return
		}
		fired = true
		items[0] = "mutated-by-listener"
	})

	require.NoError(t, cfg.Extend(auditMapLoader{
		src:  "audit-extend-payload",
		data: map[string]any{"audit": map[string]any{"items": []any{"a", "b"}}},
	}))
	require.True(t, fired, "extend listener must observe the slice payload")

	val, err := cfg.Get("audit.items")
	require.NoError(t, err)
	items, ok := val.([]any)
	require.True(t, ok)
	assert.Equal(t, "a", items[0],
		"listener mutation of the event payload must not reach live configuration")
}

// The error rendered for a rejected overwrite keeps a stable category while
// remaining discoverable through errors.Is on the wrapped cause chain.
func TestSetWithOverrideFalse_MessageRemainsOperatorFacing(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)
	defer func() { _ = cfg.Close() }()

	require.NoError(t, cfg.Set("audit.k", 1))
	err = cfg.Set("audit.k", 2, confii.WithOverride(false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	assert.False(t, errors.Is(err, confii.ErrConfigFrozen))
}

// Public inspection failures participate in the same structured error
// contract as loading, access, and mutation failures. Concrete exporter and
// filesystem causes remain available through the standard error chain.
func TestInspectionFailures_StructuredErrors(t *testing.T) {
	exporterErr := errors.New("audit exporter failed")
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithExporter(auditFailingExporter{err: exporterErr}),
	)
	require.NoError(t, err)
	defer func() { _ = cfg.Close() }()

	assertStructured := func(t *testing.T, err error, sentinel error, operation string) *confii.ConfigError {
		t.Helper()
		require.Error(t, err)
		assert.ErrorIs(t, err, sentinel)
		var configErr *confii.ConfigError
		require.ErrorAs(t, err, &configErr)
		assert.Equal(t, operation, configErr.Op)
		return configErr
	}

	t.Run("nil diff target", func(t *testing.T) {
		_, err := cfg.Diff(nil)
		_ = assertStructured(t, err, confii.ErrConfigInvalid, "Diff")
	})

	t.Run("unsupported documentation format", func(t *testing.T) {
		_, err := cfg.GenerateDocs("xml")
		configErr := assertStructured(t, err, confii.ErrConfigInvalid, "GenerateDocs")
		assert.Equal(t, "xml", configErr.Context["format"])
	})

	t.Run("documentation encoding failure", func(t *testing.T) {
		unsupported, createErr := confii.NewWithContext[any](context.Background(),
			confii.WithLoaders(auditMapLoader{
				src:  "unsupported-doc-value",
				data: map[string]any{"unsupported": make(chan int)},
			}),
		)
		require.NoError(t, createErr)
		defer func() { _ = unsupported.Close() }()

		_, err := unsupported.GenerateDocs("json")
		_ = assertStructured(t, err, confii.ErrConfigAccess, "GenerateDocs")
	})

	t.Run("unsupported export format", func(t *testing.T) {
		_, err := cfg.Export("xml")
		configErr := assertStructured(t, err, confii.ErrConfigInvalid, "Export")
		assert.Equal(t, "xml", configErr.Context["format"])
	})

	t.Run("exporter failure", func(t *testing.T) {
		_, err := cfg.Export("audit_failure")
		configErr := assertStructured(t, err, confii.ErrConfigAccess, "Export")
		assert.ErrorIs(t, err, exporterErr)
		assert.Equal(t, "audit_failure", configErr.Context["format"])
	})

	t.Run("output write failure", func(t *testing.T) {
		outputDirectory := t.TempDir()
		result, err := cfg.Export("json", outputDirectory)
		configErr := assertStructured(t, err, confii.ErrConfigAccess, "Export")
		assert.NotEmpty(t, result, "successful serialization is returned with the write error")
		assert.Equal(t, outputDirectory, configErr.Source)
		assert.Equal(t, "json", configErr.Context["format"])
	})

	t.Run("debug report write failure", func(t *testing.T) {
		outputDirectory := t.TempDir()
		err := cfg.ExportDebugReport(outputDirectory)
		configErr := assertStructured(t, err, confii.ErrConfigAccess, "ExportDebugReport")
		assert.Equal(t, outputDirectory, configErr.Source)
	})
}
