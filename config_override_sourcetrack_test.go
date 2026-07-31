// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scriptedOverrideLoader struct {
	source string
	script []scriptedOverrideStep
	calls  int32
}

type scriptedOverrideStep struct {
	data map[string]any
	err  error
}

func (s *scriptedOverrideLoader) Load(_ context.Context) (map[string]any, error) {
	idx := int(atomic.AddInt32(&s.calls, 1)) - 1
	if idx >= len(s.script) {
		idx = len(s.script) - 1
	}
	st := s.script[idx]
	return st.data, st.err
}

func (s *scriptedOverrideLoader) Source() string { return s.source }

func TestOverride_SourceTrackingParity(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	pre := cfg.Explain("database.host")
	require.Equal(t, true, pre["exists"])
	require.NotEqual(t, "override", pre["source"],
		"fixture precondition: pre-Override source must be the file loader")
	require.NotEqual(t, "runtime", pre["source"],
		"fixture precondition: pre-Override source must be the file loader")
	preSource := pre["source"]

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)

	post := cfg.Explain("database.host")
	require.Equal(t, true, post["exists"])
	assert.Equal(t, "override", post["source"],
		"Explain must report source=override after Override (F-Override-SourceTracking)")
	assert.Equal(t, "override", post["loader_type"],
		"Explain must report loader_type=override after Override (F-Override-SourceTracking)")
	assert.Equal(t, "override-value", post["current_value"])

	restore()
	after := cfg.Explain("database.host")
	require.Equal(t, true, after["exists"])
	assert.Equal(t, preSource, after["source"],
		"restore must revert Explain().source to the ORIGINAL loader source (Option A snapshot+restore())")
	assert.NotEqual(t, "override", after["source"],
		"restore must NOT leave the source as \"override\"")
	assert.NotEqual(t, "(restored)", after["source"],
		"restore must NOT use a \"(restored)\" sentinel")
	assert.NotEqual(t, "override-restored", after["source"],
		"restore must NOT use an \"override-restored\" sentinel")
}

func TestOverride_OverrideHistoryRecordsOverrideEntry(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	preHist := cfg.GetOverrideHistory("database.host")
	require.Empty(t, preHist,
		"fixture precondition: history must be empty before Override")

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)
	defer restore()

	hist := cfg.GetOverrideHistory("database.host")
	require.NotEmpty(t, hist,
		"GetOverrideHistory must contain an entry for the overridden key (F-Override-SourceTracking)")

	last := hist[len(hist)-1]
	assert.Equal(t, "localhost", last.Value,
		"history must preserve the pre-Override value (was localhost in simple.yaml)")
	assert.NotEqual(t, "override", last.Source,
		"history entry source must be the pre-Override source, not \"override\"")

	info := cfg.GetSourceInfo("database.host")
	require.NotNil(t, info)
	assert.Equal(t, "override", info.SourceFile)
	assert.Equal(t, "override", info.LoaderType)
	assert.GreaterOrEqual(t, info.OverrideCount, 1,
		"OverrideCount must increment on Override (F-Override-SourceTracking)")
}

func TestOverride_RestoreTruncatesOverrideHistory(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithDebugMode(true),
	)
	require.NoError(t, err)

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)

	require.NotEmpty(t, cfg.GetOverrideHistory("database.host"))

	restore()

	postHist := cfg.GetOverrideHistory("database.host")
	assert.Empty(t, postHist,
		"restore must truncate the override history (Option A snapshot+restore())")

	info := cfg.GetSourceInfo("database.host")
	require.NotNil(t, info)
	assert.Equal(t, 0, info.OverrideCount,
		"restore must reset OverrideCount to the pre-Override value (0)")
	assert.NotEqual(t, "override", info.SourceFile,
		"restore must revert SourceFile to the pre-Override loader source")
}

func TestOverride_ConcurrentOverrideAndReload(t *testing.T) {
	l := &scriptedOverrideLoader{
		source: "scripted-loader",
		script: []scriptedOverrideStep{
			{data: map[string]any{"k": "v1"}},
			{data: map[string]any{"k": "v2"}},
			{data: map[string]any{"k": "v2"}},
		},
	}
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(l),
	)
	require.NoError(t, err)

	restore, err := cfg.Override(map[string]any{"k": "override-value"})
	require.NoError(t, err)
	defer restore()

	assert.Equal(t, "override", cfg.Explain("k")["source"])

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = cfg.Override(map[string]any{"k": "override-2"})
	}()
	go func() {
		defer wg.Done()
		_ = cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false))
	}()
	wg.Wait()

	require.NoError(t, cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false)))

	post := cfg.Explain("k")
	require.Equal(t, true, post["exists"])
	assert.Equal(t, "scripted-loader", post["source"],
		"a Reload after Override must overwrite the override source claim with the loader source")
}

func TestOverride_ConcurrentOverrideAndSet(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	preSource := cfg.Explain("database.host")["source"]

	restore, err := cfg.Override(map[string]any{
		"database.host": "override-value",
	})
	require.NoError(t, err)
	require.NotNil(t, restore)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = cfg.Set("database.host", "set-value")
	}()
	go func() {
		defer wg.Done()

		_, _ = cfg.Override(map[string]any{"database.port": 6543})
	}()
	wg.Wait()

	restore()

	after := cfg.Explain("database.host")
	require.Equal(t, true, after["exists"])
	assert.Equal(t, preSource, after["source"],
		"restore must revert to the pre-Override (loader) source even after a concurrent Set")
}

func TestOverride_FailingReloadRollbackPreservesContract(t *testing.T) {
	l := &scriptedOverrideLoader{
		source: "scripted-loader",
		script: []scriptedOverrideStep{
			{data: map[string]any{"k": "v1"}},
			{err: errors.New("simulated reload failure")},
		},
	}
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(l),
		confii.WithOnError(confii.ErrorPolicyRaise),
	)
	require.NoError(t, err)

	pre := cfg.Explain("k")
	require.Equal(t, "scripted-loader", pre["source"])

	restore, err := cfg.Override(map[string]any{"k": "override-value"})
	require.NoError(t, err)
	require.NotNil(t, restore)

	mid := cfg.Explain("k")
	require.Equal(t, "override", mid["source"])
	require.Equal(t, "override-value", mid["current_value"])

	err = cfg.ReloadWithContext(context.Background(), confii.WithIncremental(false))
	require.Error(t, err, "Reload must propagate the loader failure")

	postReload := cfg.Explain("k")
	assert.Equal(t, true, postReload["exists"])
	assert.Equal(t, "override", postReload["source"],
		"rollback must preserve source tracking after an override and failed reload")
	assert.Equal(t, "override-value", postReload["current_value"],
		"rollback must preserve configuration after an override and failed reload")

	restore()

	postRestore := cfg.Explain("k")
	assert.Equal(t, "scripted-loader", postRestore["source"],
		"restore after a failing Reload must still revert to the pre-Override loader source")
}
