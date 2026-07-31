// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refactorCoverageLoader struct {
	source string
	data   map[string]any
	err    error
}

func (l *refactorCoverageLoader) Source() string { return l.source }

func (l *refactorCoverageLoader) Load(context.Context) (map[string]any, error) {
	return l.data, l.err
}

func TestConfig_LoadAndAccessFailureBranches(t *testing.T) {
	loadErr := errors.New("load failed")
	_, err := NewWithContext[any](context.Background(),
		WithLoaders(&refactorCoverageLoader{source: "broken", err: loadErr}),
		WithOnError(ErrorPolicy("invalid")),
	)
	require.ErrorIs(t, err, loadErr)
	_, err = NewWithContext[any](context.Background(),
		WithLoaders(&refactorCoverageLoader{source: "compose.yaml", data: map[string]any{"_include": "missing.yaml"}}),
		WithOnError(ErrorPolicy("invalid")),
	)
	require.Error(t, err)

	t.Setenv("REFACTOR_MISSING", "value")
	cfg := newTestConfig(t, map[string]any{
		"huge":    math.MaxFloat64,
		"badbool": int64(7),
	}, WithSysenvFallback(true), WithKeyHook("refactor.missing", func(context.Context, string, any) (any, error) {
		return nil, errors.New("hook failed")
	}))
	_, err = cfg.GetWithContext(context.Background(), "refactor.missing")
	require.EqualError(t, err, "hook failed")

	_, err = cfg.GetInt("huge")
	require.Error(t, err)
	_, err = cfg.GetBool("badbool")
	require.Error(t, err)

	cfg.mu.Lock()
	cfg.envConfig = nil
	cfg.mergedConfig = map[string]any{"fallback": "merged"}
	cfg.mu.Unlock()
	got, err := cfg.ToDictWithContext(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "merged", got["fallback"])
}

func TestSet_RollsBackWhenMergedConfigPathFails(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{})
	cfg.EnableObservability()
	cfg.EnableEvents()
	cfg.mu.Lock()
	cfg.envConfig = map[string]any{}
	cfg.mergedConfig = map[string]any{"parent": "scalar"}
	cfg.mu.Unlock()

	err := cfg.Set("parent.child", "value")
	require.Error(t, err)
	data, dictErr := cfg.ToDict()
	require.NoError(t, dictErr)
	assert.Empty(t, data)
}

func TestExtend_PoliciesCompositionAndSchemaRollback(t *testing.T) {
	loadErr := errors.New("extend load failed")
	cfg := newTestConfig(t, map[string]any{"stable": true})
	cfg.EnableObservability()
	cfg.EnableEvents()
	cfg.opts.OnError = ErrorPolicy("invalid")
	err := cfg.ExtendWithContext(context.Background(), &refactorCoverageLoader{source: "broken", err: loadErr})
	require.ErrorIs(t, err, loadErr)

	for _, policy := range []ErrorPolicy{ErrorPolicyRaise, ErrorPolicyWarn, ErrorPolicyIgnore, ErrorPolicy("invalid")} {
		t.Run(fmt.Sprintf("policy-%s", policy), func(t *testing.T) {
			candidate := newTestConfig(t, map[string]any{"stable": true})
			candidate.EnableObservability()
			candidate.EnableEvents()
			candidate.opts.OnError = policy
			err := candidate.ExtendWithContext(context.Background(), &refactorCoverageLoader{
				source: "compose.yaml",
				data: map[string]any{
					"_include": "missing.yaml",
					"added":    true,
				},
			})
			if policy == ErrorPolicyRaise || policy == ErrorPolicy("invalid") {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}

	dependencyCfg := newTestConfig(t, map[string]any{})
	dependencyCfg.opts.OnError = ErrorPolicyWarn
	err = dependencyCfg.ExtendWithContext(context.Background(), &refactorCoverageLoader{
		source: "dependency.yaml",
		data: map[string]any{
			"_include": filepath.Join(t.TempDir(), "missing.yaml"),
		},
	})
	require.NoError(t, err)
	dependencyCfg.trackCompositionDependency(filepath.Join(t.TempDir(), "removed.yaml"))

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	validated, err := NewWithContext[any](context.Background(),
		WithLoaders(&refactorCoverageLoader{source: "base", data: map[string]any{"name": "valid"}}),
		WithSchema(schema),
		WithValidateOnLoad(true),
	)
	require.NoError(t, err)
	err = validated.ExtendWithContext(context.Background(), &refactorCoverageLoader{source: "invalid", data: map[string]any{"name": 42}})
	require.ErrorIs(t, err, ErrConfigValidation)
}

func TestOverride_FailureAndReplayBranches(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{})
	cfg.EnableObservability()
	cfg.EnableEvents()
	cfg.mu.Lock()
	cfg.envConfig = map[string]any{}
	cfg.mergedConfig = map[string]any{"parent": "scalar"}
	cfg.mu.Unlock()

	_, err := cfg.Override(map[string]any{"parent.child": "value"})
	require.Error(t, err)

	replay := newTestConfig(t, map[string]any{"shape": map[string]any{"leaf": "base"}})
	replay.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	replay.overrideBaseEnv = map[string]any{"shape": "blocked"}
	replay.overrideBaseMerged = map[string]any{"shape": "blocked"}
	replay.overrideBaseTracker = replay.sourceTracker.Snapshot()
	removed := &overrideFrame{id: 1, payload: map[string]any{"other": true}, applied: true}
	survivor := &overrideFrame{id: 2, payload: map[string]any{"shape.leaf": "override"}, applied: true}
	replay.overrideStack = []*overrideFrame{removed, survivor}
	restore := replay.makeOverrideRestore(removed)
	restore()
	assert.Len(t, replay.overrideStack, 1)

	replayMerged := newTestConfig(t, map[string]any{"shape": map[string]any{"leaf": "base"}})
	replayMerged.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	replayMerged.overrideBaseEnv = map[string]any{"shape": map[string]any{"leaf": "base"}}
	replayMerged.overrideBaseMerged = map[string]any{"shape": "blocked"}
	replayMerged.overrideBaseTracker = replayMerged.sourceTracker.Snapshot()
	removed = &overrideFrame{id: 3, payload: map[string]any{"other": true}, applied: true}
	survivor = &overrideFrame{id: 4, payload: map[string]any{"shape.leaf": "override"}, applied: true}
	replayMerged.overrideStack = []*overrideFrame{removed, survivor}
	replayMerged.makeOverrideRestore(removed)()
	assert.Len(t, replayMerged.overrideStack, 1)
}

func TestExportAndHookFailureBranches(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"unsupported": make(chan int)})
	_, err := cfg.ExportWithContext(context.Background(), "json")
	require.Error(t, err)

	nilMap, err := cfg.applySecretHookRecursive(context.Background(), "", nil)
	require.NoError(t, err)
	assert.Nil(t, nilMap)

	failing := hook.NewProcessor()
	failing.RegisterGlobalHook(func(context.Context, string, any) (any, error) {
		return nil, errors.New("nested hook failed")
	})
	cfg.hookProcessor = failing
	_, err = cfg.applySecretHookRecursive(context.Background(), "root", map[string]any{
		"nested": map[string]any{"bad": "value"},
	})
	require.EqualError(t, err, "nested hook failed")
	_, err = cfg.applySecretHookRecursive(context.Background(), "root", map[string]any{
		"nested": []any{"value"},
	})
	require.EqualError(t, err, "nested hook failed")
	_, err = cfg.applySecretHookToSlice(context.Background(), "root.bad", []any{"value"})
	require.EqualError(t, err, "nested hook failed")
	_, err = cfg.applySecretHookToSlice(context.Background(), "root", []any{map[string]any{"bad": "value"}})
	require.EqualError(t, err, "nested hook failed")
	_, err = cfg.applySecretHookToSlice(context.Background(), "root", []any{[]any{map[string]any{"bad": "value"}}})
	require.EqualError(t, err, "nested hook failed")
	cfg.hookProcessor = hook.NewProcessor()
	gotSlice, err := cfg.applySecretHookToSlice(context.Background(), "root", []any{[]any{"value"}})
	require.NoError(t, err)
	assert.Equal(t, []any{[]any{"value"}}, gotSlice)
}

func TestConfig_InternalHelperContracts(t *testing.T) {
	assert.Empty(t, loaderTypeName(nil))
	anonymous := struct{ Loader }{Loader: &refactorCoverageLoader{source: "embedded"}}
	assert.Contains(t, loaderTypeName(anonymous), "struct")

	for input, want := range map[string]slog.Level{
		"info":    slog.LevelInfo,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	} {
		got, err := parseSelfConfigLogLevel(input)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	base := defaultOptions()
	ctx := context.Background()
	require.Error(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "yaml"}))
	require.NoError(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "dotenv", "path": "app.env"}))
	require.Error(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "dotenv"}))
	require.Error(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "env", "prefix": "app"}))
	require.Error(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "environment"}))
	require.NoError(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "environment", "prefix": "other"}))
	require.NoError(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "environment", "prefix": "OTHER"}))
	require.Error(t, appendSelfConfigSource(ctx, &base, map[string]any{"type": "unknown"}))

	badSchema := defaultOptions()
	badSchema.Schema = map[string]any{"type": "definitely-not-a-json-schema-type"}
	_, err := resolveSchemaValidator(&badSchema)
	require.Error(t, err)

	type pointerConfig struct{ Name string }
	assert.True(t, configTypeSupportsStructValidation[*pointerConfig]())

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".confii.yaml"), []byte("invalid: ["), 0o600))
	selfconfig.ClearCache()
	_, err = NewWithContext[any](context.Background(), WithWorkingDir(dir), WithLoaders())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConfigLoad)
}

func TestDeclarativeSourceFormatCanonicalTypesAndExtensions(t *testing.T) {
	valid := []struct {
		typeName string
		path     string
	}{
		{"yaml", "config.yaml"},
		{"yaml", "config.yml"},
		{"json", "config.json"},
		{"toml", "config.toml"},
		{"ini", "config.ini"},
		{"ini", "config.cfg"},
		{"dotenv", ".env"},
		{"dotenv", ".env.production"},
		{"dotenv", "secrets.env"},
	}
	for _, tt := range valid {
		t.Run(tt.typeName+"/"+tt.path, func(t *testing.T) {
			format, err := declarativeSourceFormat(tt.typeName, tt.path)
			require.NoError(t, err)
			assert.NotEmpty(t, format)
		})
	}

	for _, tt := range []struct {
		typeName string
		path     string
	}{
		{"yaml", "config.json"},
		{"json", "config.yaml"},
		{"toml", "config.ini"},
		{"ini", "config.toml"},
		{"dotenv", "config.yaml"},
	} {
		t.Run("reject/"+tt.typeName+"/"+tt.path, func(t *testing.T) {
			_, err := declarativeSourceFormat(tt.typeName, tt.path)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrConfigFormat)
			assert.Contains(t, err.Error(), "incompatible")
		})
	}
}

func TestDeclarativeSourceAliasesAreRejected(t *testing.T) {
	for _, alias := range []string{"yml", "cfg", "env", "envfile", "env-vars", "environment-files"} {
		t.Run(alias, func(t *testing.T) {
			opts := defaultOptions()
			source := map[string]any{"type": alias, "path": "config.yaml", "prefix": "APP"}
			err := appendSelfConfigSource(context.Background(), &opts, source)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrConfigLoad)
			assert.Contains(t, err.Error(), "unsupported self-config source type")
		})
	}
}

func TestFileAutoLoader_ErrorAndFormatBranches(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	write := func(name, contents string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
		return path
	}

	loop := filepath.Join(dir, "loop().yaml")
	require.NoError(t, os.Symlink("loop().yaml", loop))
	_, err := (&fileAutoLoader{path: loop}).Load(context.Background())
	require.Error(t, err)

	for _, name := range []string{"read.yaml", "read.json", "read.toml"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.Mkdir(path, 0o700))
		loader := &fileAutoLoader{path: path}
		_, err = loader.Load(context.Background())
		require.Error(t, err)
	}

	loader := &fileAutoLoader{logger: logger}
	loader.path = write("empty.yaml", "")
	got, err := loader.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
	loader.path = write("collision.yaml", "null: value\n")
	_, err = loader.Load(context.Background())
	require.Error(t, err)
	loader.path = write("scalar.yaml", "- one\n- two\n")
	_, err = loader.Load(context.Background())
	require.Error(t, err)
	loader.path = write("bad.json", "{")
	_, err = loader.Load(context.Background())
	require.Error(t, err)
	loader.path = write("bad.toml", "invalid = [")
	_, err = loader.Load(context.Background())
	require.Error(t, err)
	loader.path = write("empty.ini", "")
	got, err = loader.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
	iniDir := filepath.Join(dir, "directory.ini")
	require.NoError(t, os.Mkdir(iniDir, 0o700))
	loader.path = iniDir
	_, err = loader.Load(context.Background())
	require.Error(t, err)

	malformed := write("malformed.env", "MISSING_EQUALS\n")
	for _, policy := range []ErrorPolicy{ErrorPolicyIgnore, ErrorPolicyWarn, ErrorPolicyRaise} {
		loader = &fileAutoLoader{path: malformed, errorPolicy: policy, logger: logger}
		got, err = loader.Load(context.Background())
		if policy == ErrorPolicyRaise {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Nil(t, got)
		}
	}

	loader = &fileAutoLoader{path: write("values.env", "SINGLE='literal'\nDOUBLE=\"line\\nvalue\\tend\"\nPLAIN=value # comment\nNESTED.KEY=1\n")}
	got, err = loader.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "literal", got["SINGLE"])
	assert.Equal(t, "line\nvalue\tend", got["DOUBLE"])
	assert.Equal(t, "value", got["PLAIN"])
	nested, ok := dictutil.GetNested(got, "NESTED.KEY")
	require.True(t, ok)
	assert.Equal(t, 1, nested)

	loader = &fileAutoLoader{path: malformed, errorPolicy: ErrorPolicyWarn}
	_, err = loader.Load(context.Background())
	require.NoError(t, err)

	missingErr := os.ErrNotExist
	for _, policy := range []ErrorPolicy{ErrorPolicyIgnore, ErrorPolicyWarn, ErrorPolicyRaise, ErrorPolicy("invalid")} {
		loader = &fileAutoLoader{path: "missing", errorPolicy: policy}
		got, err = loader.handleMissing(missingErr, logger)
		if policy == ErrorPolicyIgnore || policy == ErrorPolicyWarn {
			require.NoError(t, err)
			assert.Nil(t, got)
		} else {
			require.Error(t, err)
		}
	}

}

func TestConfig_WatchAndTypedCacheBranches(t *testing.T) {
	nonFile := newTestConfig(t, map[string]any{})
	nonFile.loaders = []Loader{&refactorCoverageLoader{source: "environment:APP"}}
	nonFile.startWatching()
	assert.Nil(t, nonFile.watcher)

	type typedModel struct {
		Name string `confii:"name"`
	}
	cfg, err := NewWithContext[typedModel](context.Background(),
		WithLoaders(&refactorCoverageLoader{source: "typed", data: map[string]any{"name": "from-config"}}),
		WithGlobalHook(func(_ context.Context, _ string, value any) (any, error) {
			if value == "from-config" {
				return "materialized", nil
			}
			return value, nil
		}),
	)
	require.NoError(t, err)
	initial, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "materialized", initial.Name)
	model, err := cfg.Typed()
	require.NoError(t, err)
	assert.Equal(t, "materialized", model.Name)
	assert.Same(t, initial, model)
	cached, err := cfg.Typed()
	require.NoError(t, err)
	assert.Same(t, model, cached)
}
