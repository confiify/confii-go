// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scriptedLoader struct {
	source string
	script []scriptStep
	calls  int32
}

type scriptStep struct {
	data map[string]any
	err  error
}

func (s *scriptedLoader) Load(_ context.Context) (map[string]any, error) {
	idx := int(atomic.AddInt32(&s.calls, 1)) - 1
	if idx >= len(s.script) {
		idx = len(s.script) - 1
	}
	st := s.script[idx]
	return st.data, st.err
}

func (s *scriptedLoader) Source() string { return s.source }

func TestNew_FreezeOnLoadAndDynamicReloading_RejectsWithTypedError(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "k: v\n")
	_, err := NewWithContext[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
		WithFreezeOnLoad(true),
		WithDynamicReloading(true),
	)
	require.Error(t, err, "FreezeOnLoad+DynamicReloading must be rejected ")

	var ce *ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.True(t, errors.Is(err, ErrConfigInvalid),
		"conflict error must wrap ErrConfigInvalid")
	assert.Contains(t, err.Error(), "WithFreezeOnLoad")
	assert.Contains(t, err.Error(), "WithDynamicReloading")
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestNew_FreezeOnLoadOnly_StillWorks(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "k: v\n")
	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
		WithFreezeOnLoad(true),
	)
	require.NoError(t, err)
	assert.True(t, cfg.IsFrozen())
}

func TestNew_DynamicReloadingOnly_StillWorks(t *testing.T) {
	path := writeTempYAML(t, "cfg.yaml", "k: v\n")
	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
		WithDynamicReloading(true),
	)
	require.NoError(t, err)
	defer cfg.StopWatching()
	assert.False(t, cfg.IsFrozen())
}

type recordingSink struct {
	mu     sync.Mutex
	events []string
}

func (r *recordingSink) record(name string) func(args ...any) {
	return func(args ...any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.events = append(r.events, name)
	}
}

func (r *recordingSink) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

func TestReload_MetricsAndEventsEmittedOnlyOnCommit(t *testing.T) {
	type AppCfg struct {
		Required string `confii:"required" validate:"required"`
	}

	loader := &scriptedLoader{
		source: "scripted",
		script: []scriptStep{
			{data: map[string]any{"required": "ok"}},
			{data: map[string]any{"other": "missing-req"}},
		},
	}

	cfg, err := NewWithContext[AppCfg](context.Background(), WithLoaders(loader))
	require.NoError(t, err)

	sink := &recordingSink{}
	emitter := cfg.EnableEvents()
	emitter.On("reload", sink.record("reload"))
	emitter.On("reload_failed", sink.record("reload_failed"))
	emitter.On("change", sink.record("change"))

	metrics := cfg.EnableObservability()

	err = cfg.ReloadWithContext(context.Background(),
		WithReloadValidate(true),
		WithIncremental(false),
	)
	require.Error(t, err, "validation failure must surface as Reload error")
	assert.True(t, errors.Is(err, ErrConfigValidation),
		"expected ErrConfigValidation, got %v", err)

	got := sink.snapshot()
	assert.NotContains(t, got, "reload",
		"reload event must NOT fire when validation rollback occurred ")
	assert.NotContains(t, got, "change",
		"change event must NOT fire when validation rollback occurred ")
	assert.Contains(t, got, "reload_failed",
		"reload_failed event must fire on validation rollback ")

	stats := metrics.Statistics()
	assert.Equal(t, 0, stats["reload_count"],
		"reload_count must NOT increment on validation rollback ")
	assert.Equal(t, 1, stats["reload_failed_count"],
		"reload_failed_count must increment on validation rollback ")
}

func TestReload_LoaderErrorEmitsReloadFailedEventAndMetric(t *testing.T) {
	loader := &scriptedLoader{
		source: "scripted",
		script: []scriptStep{
			{data: map[string]any{"k": "original"}},
			{err: errors.New("loader-failed-on-reload")},
		},
	}

	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(loader),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)

	sink := &recordingSink{}
	emitter := cfg.EnableEvents()
	emitter.On("reload", sink.record("reload"))
	emitter.On("reload_failed", sink.record("reload_failed"))
	emitter.On("change", sink.record("change"))
	metrics := cfg.EnableObservability()

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loader-failed-on-reload")

	got := sink.snapshot()
	assert.NotContains(t, got, "reload")
	assert.NotContains(t, got, "change")
	assert.Contains(t, got, "reload_failed")

	stats := metrics.Statistics()
	assert.Equal(t, 0, stats["reload_count"])
	assert.Equal(t, 1, stats["reload_failed_count"])

	v, gerr := cfg.Get("k")
	require.NoError(t, gerr)
	assert.Equal(t, "original", v)
}

func TestReload_SuccessfulCommitEmitsReloadAndChange(t *testing.T) {
	loader := &scriptedLoader{
		source: "scripted",
		script: []scriptStep{
			{data: map[string]any{"k": "v1"}},
			{data: map[string]any{"k": "v2"}},
		},
	}

	cfg, err := NewWithContext[any](context.Background(), WithLoaders(loader))
	require.NoError(t, err)

	sink := &recordingSink{}
	emitter := cfg.EnableEvents()
	emitter.On("reload", sink.record("reload"))
	emitter.On("reload_failed", sink.record("reload_failed"))
	emitter.On("change", sink.record("change"))
	metrics := cfg.EnableObservability()

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.NoError(t, err)

	got := sink.snapshot()
	assert.Contains(t, got, "reload")
	assert.Contains(t, got, "change")
	assert.NotContains(t, got, "reload_failed")

	require.Len(t, got, 2)
	assert.Equal(t, []string{"reload", "change"}, got,
		"reload event must precede change event ")

	stats := metrics.Statistics()
	assert.Equal(t, 1, stats["reload_count"])
	assert.Equal(t, 0, stats["reload_failed_count"])
}

func TestReload_DryRunEmitsNoReloadEvent(t *testing.T) {
	loader := &scriptedLoader{
		source: "scripted",
		script: []scriptStep{
			{data: map[string]any{"k": "v1"}},
			{data: map[string]any{"k": "v2"}},
		},
	}

	cfg, err := NewWithContext[any](context.Background(), WithLoaders(loader))
	require.NoError(t, err)

	sink := &recordingSink{}
	emitter := cfg.EnableEvents()
	emitter.On("reload", sink.record("reload"))
	emitter.On("reload_failed", sink.record("reload_failed"))
	emitter.On("change", sink.record("change"))
	metrics := cfg.EnableObservability()

	err = cfg.ReloadWithContext(context.Background(),
		WithDryRun(true),
		WithIncremental(false),
	)
	require.NoError(t, err)

	got := sink.snapshot()
	assert.NotContains(t, got, "reload",
		"dry-run must NOT emit reload event ")
	assert.NotContains(t, got, "change",
		"dry-run must NOT emit change event ")
	assert.NotContains(t, got, "reload_failed",
		"successful dry-run must NOT emit reload_failed either ")

	stats := metrics.Statistics()
	assert.Equal(t, 0, stats["reload_count"])
	assert.Equal(t, 0, stats["reload_failed_count"])

	v, _ := cfg.Get("k")
	assert.Equal(t, "v1", v)
}

func TestReload_IncrementalDryRunValidationFires(t *testing.T) {
	type AppCfg struct {
		Required string `confii:"required" validate:"required"`
	}

	path := writeTempYAML(t, "cfg.yaml", "required: ok\n")
	cfg, err := NewWithContext[AppCfg](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
	)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, []byte("other: missing-req\n"), 0644))

	err = cfg.ReloadWithContext(context.Background(), WithReloadValidate(true))
	require.Error(t, err, "incremental reload must run validation when files change ")
	assert.True(t, errors.Is(err, ErrConfigValidation))

	v, gerr := cfg.Get("required")
	require.NoError(t, gerr)
	assert.Equal(t, "ok", v)
}

func TestReload_GateSemanticsDocumented(t *testing.T) {
	loader := &scriptedLoader{
		source: "scripted",
		script: []scriptStep{
			{data: map[string]any{"k": "v"}},
		},
	}
	cfg, err := NewWithContext[any](context.Background(), WithLoaders(loader))
	require.NoError(t, err)

	initial := atomic.LoadInt32(&loader.calls)
	require.Equal(t, int32(1), initial, "initial New invokes Load once")

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(true))
	require.NoError(t, err)

	_ = atomic.LoadInt32(&loader.calls)

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.NoError(t, err)
	after := atomic.LoadInt32(&loader.calls)
	assert.Greater(t, after, initial,
		"WithIncremental(false) must run all loaders even when nothing changed")
}

func TestReload_SourceTrackerRolledBackOnLoaderError(t *testing.T) {

	l1 := &stubLoader{source: "loader-1", data: map[string]any{"a": 1}}

	l2 := &scriptedLoader{
		source: "loader-2",
		script: []scriptStep{
			{data: map[string]any{"b": 2}},
			{err: errors.New("loader-2-broke")},
		},
	}

	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(l1, l2),
		WithOnError(ErrorPolicyRaise),
		WithDebugMode(true),
	)
	require.NoError(t, err)

	preInfoA := cfg.GetSourceInfo("a")
	preInfoB := cfg.GetSourceInfo("b")
	require.NotNil(t, preInfoA)
	require.NotNil(t, preInfoB)
	preCountA := preInfoA.OverrideCount
	preCountB := preInfoB.OverrideCount
	preSourceA := preInfoA.SourceFile
	preSourceB := preInfoB.SourceFile
	preStats := cfg.GetSourceStatistics()
	preTotalKeys := preStats["total_keys"]
	preTotalOverrides := preStats["total_overrides"]

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loader-2-broke")

	postInfoA := cfg.GetSourceInfo("a")
	postInfoB := cfg.GetSourceInfo("b")
	require.NotNil(t, postInfoA)
	require.NotNil(t, postInfoB)
	assert.Equal(t, preCountA, postInfoA.OverrideCount,
		"override_count for 'a' must not grow after rolled-back reload ")
	assert.Equal(t, preCountB, postInfoB.OverrideCount,
		"override_count for 'b' must not grow after rolled-back reload ")
	assert.Equal(t, preSourceA, postInfoA.SourceFile)
	assert.Equal(t, preSourceB, postInfoB.SourceFile)

	postStats := cfg.GetSourceStatistics()
	assert.Equal(t, preTotalOverrides, postStats["total_overrides"],
		"aggregate total_overrides must not grow after rolled-back reload ")
	assert.Equal(t, preTotalKeys, postStats["total_keys"],
		"total_keys must not grow after rolled-back reload ")
}

func TestReload_PolicyMatrix_LoaderError_Raise(t *testing.T) {
	l := &scriptedLoader{
		source: "loader",
		script: []scriptStep{
			{data: map[string]any{"k": "v0"}},
			{err: errors.New("raise-me")},
		},
	}
	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(l),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "raise-me")

	v, _ := cfg.Get("k")
	assert.Equal(t, "v0", v)
}

func TestReload_PolicyMatrix_LoaderError_Warn(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()
	bad := &scriptedLoader{
		source: "bad",
		script: []scriptStep{
			{data: map[string]any{"k1": "ok"}},
			{err: errors.New("warn-on-reload")},
		},
	}
	good := &stubLoader{source: "good", data: map[string]any{"k2": "good-v"}}

	opts := append([]Option{
		WithLoaders(bad, good),
		WithOnError(ErrorPolicyWarn),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)

	logBuf.Reset()
	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.NoError(t, err, "Warn policy commits despite loader failure")

	out := logBuf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "loader error")
	assert.Contains(t, out, "warn-on-reload")
}

func TestReload_PolicyMatrix_LoaderError_Ignore(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()
	bad := &scriptedLoader{
		source: "bad",
		script: []scriptStep{
			{data: map[string]any{"k1": "ok"}},
			{err: errors.New("ignore-on-reload")},
		},
	}
	good := &stubLoader{source: "good", data: map[string]any{"k2": "good-v"}}

	opts := append([]Option{
		WithLoaders(bad, good),
		WithOnError(ErrorPolicyIgnore),
	}, logOpts...)

	cfg, err := NewWithContext[any](context.Background(), opts...)
	require.NoError(t, err)

	logBuf.Reset()
	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.NoError(t, err)

	assert.NotContains(t, logBuf.String(), "loader error",
		"ErrorPolicyIgnore must not produce a 'loader error' log record on Reload ")
	assert.NotContains(t, logBuf.String(), "ignore-on-reload")
}

func TestReload_PolicyMatrix_CompositionError_Raise(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("k: v\n"), 0o600))

	cfg, err := NewWithContext[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: cfgPath}),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)

	broken := "_include:\n  - not_exist_g14_compose.yaml\nk: changed\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(broken), 0o600))

	err = cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	require.Error(t, err, "composer error must surface from Reload under Raise ")

	v, _ := cfg.Get("k")
	assert.Equal(t, "v", v)
}

func TestReload_PolicyMatrix_ValidationError_Raise(t *testing.T) {
	type AppCfg struct {
		Required string `confii:"required" validate:"required"`
	}
	loader := &scriptedLoader{
		source: "scripted",
		script: []scriptStep{
			{data: map[string]any{"required": "ok"}},
			{data: map[string]any{"other": "missing"}},
		},
	}
	cfg, err := NewWithContext[AppCfg](context.Background(),
		WithLoaders(loader),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)

	err = cfg.ReloadWithContext(context.Background(),
		WithReloadValidate(true),
		WithIncremental(false),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConfigValidation))
}
