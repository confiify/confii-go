// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingJSONFileLoader struct {
	path  string
	calls int
}

type gatedLoader struct {
	source  string
	data    map[string]any
	started chan struct{}
	release chan struct{}
}

func (l *gatedLoader) Load(ctx context.Context) (map[string]any, error) {
	close(l.started)
	select {
	case <-l.release:
		return l.data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *gatedLoader) Source() string { return l.source }

type nonCancelableGatedLoader struct {
	source  string
	data    map[string]any
	started chan struct{}
	release chan struct{}
}

func (l *nonCancelableGatedLoader) Load(context.Context) (map[string]any, error) {
	close(l.started)
	<-l.release
	return l.data, nil
}

func (l *nonCancelableGatedLoader) Source() string { return l.source }

type gatedReloadLoader struct {
	calls   int
	started chan struct{}
	release chan struct{}
}

func (l *gatedReloadLoader) Load(ctx context.Context) (map[string]any, error) {
	l.calls++
	if l.calls == 1 {
		return map[string]any{"key": "before"}, nil
	}
	close(l.started)
	select {
	case <-l.release:
		return map[string]any{"key": "after"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *gatedReloadLoader) Source() string { return "gated-reload" }

type conflictingReloadLoader struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (l *conflictingReloadLoader) Load(ctx context.Context) (map[string]any, error) {
	switch l.calls.Add(1) {
	case 1:
		return map[string]any{"key": "before"}, nil
	case 2:
		close(l.started)
		select {
		case <-l.release:
			return map[string]any{"key": "superseded"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	default:
		return map[string]any{"key": "after-retry"}, nil
	}
}

func (l *conflictingReloadLoader) Source() string { return "conflicting-reload" }

func configRevision[T any](c *Config[T]) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.revision
}

func (l *countingJSONFileLoader) Load(context.Context) (map[string]any, error) {
	l.calls++
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (l *countingJSONFileLoader) Source() string { return l.path }

func requireCompletes(t *testing.T, operation func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation deadlocked while invoking a re-entrant event listener")
		return nil
	}
}

func TestLifecycleEventsMayReadConfigWithoutDeadlock(t *testing.T) {
	t.Run("reload_success", func(t *testing.T) {
		l := &scriptedLoader{source: "scripted", script: []scriptStep{
			{data: map[string]any{"key": "before"}},
			{data: map[string]any{"key": "after"}},
		}}
		cfg, err := NewWithContext[any](context.Background(), WithLoaders(l))
		require.NoError(t, err)
		cfg.EnableEvents().On("reload", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "after", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.ReloadWithContext(context.Background(), WithIncremental(false))
		})
		require.NoError(t, err)
	})

	t.Run("reload_failure", func(t *testing.T) {
		l := &scriptedLoader{source: "scripted", script: []scriptStep{
			{data: map[string]any{"key": "before"}},
			{err: errors.New("reload failed")},
		}}
		cfg, err := NewWithContext[any](context.Background(), WithLoaders(l))
		require.NoError(t, err)
		cfg.EnableEvents().On("reload_failed", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "before", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.ReloadWithContext(context.Background(), WithIncremental(false))
		})
		require.Error(t, err)
	})

	t.Run("extend_success", func(t *testing.T) {
		cfg, err := NewWithContext[any](context.Background(), WithLoaders(&stubLoader{source: "base", data: map[string]any{"key": "before"}}))
		require.NoError(t, err)
		cfg.EnableEvents().On("extend", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "after", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.ExtendWithContext(context.Background(), &stubLoader{
				source: "extension", data: map[string]any{"key": "after"},
			})
		})
		require.NoError(t, err)
	})

	t.Run("extend_failure", func(t *testing.T) {
		cfg, err := NewWithContext[any](context.Background(), WithLoaders(&stubLoader{source: "base", data: map[string]any{"key": "before"}}))
		require.NoError(t, err)
		cfg.EnableEvents().On("extend_failed", func(_ ...any) {
			value, getErr := cfg.Get("key")
			require.NoError(t, getErr)
			assert.Equal(t, "before", value)
		})

		err = requireCompletes(t, func() error {
			return cfg.ExtendWithContext(context.Background(), &stubLoader{
				source: "broken", err: errors.New("extend failed"),
			})
		})
		require.Error(t, err)
	})
}

func TestExtendNoOpDoesNotPublishOrEmitLifecycleSignals(t *testing.T) {
	tests := []struct {
		name   string
		policy ErrorPolicy
		loader Loader
	}{
		{
			name:   "warned loader failure",
			policy: ErrorPolicyWarn,
			loader: &stubLoader{source: "warned", err: errors.New("unavailable")},
		},
		{
			name:   "ignored loader failure",
			policy: ErrorPolicyIgnore,
			loader: &stubLoader{source: "ignored", err: errors.New("unavailable")},
		},
		{
			name:   "empty loader result",
			policy: ErrorPolicyRaise,
			loader: &stubLoader{source: "empty", data: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := NewWithContext[any](context.Background(),
				WithLoaders(&stubLoader{source: "base", data: map[string]any{"key": "before"}}),
				WithOnError(tt.policy),
			)
			require.NoError(t, err)
			metrics := cfg.EnableObservability()
			events := cfg.EnableEvents()
			var emitted []string
			events.On("extend", func(_ ...any) { emitted = append(emitted, "extend") })
			events.On("change", func(_ ...any) { emitted = append(emitted, "change") })
			callbackCount := 0
			cfg.OnChange(func(string, any, any) { callbackCount++ })

			beforeRevision := configRevision(cfg)
			beforeLoaders := len(cfg.loaders)
			require.NoError(t, cfg.ExtendWithContext(context.Background(), tt.loader))

			assert.Equal(t, beforeRevision, configRevision(cfg))
			assert.Len(t, cfg.loaders, beforeLoaders)
			assert.Empty(t, emitted)
			assert.Zero(t, callbackCount)
			stats := metrics.Statistics()
			assert.Equal(t, 0, stats["extend_count"])
			assert.Equal(t, 0, stats["change_count"])
		})
	}
}

func TestSourceTransactionsRejectPublicationAfterConcurrentFreeze(t *testing.T) {
	t.Run("reload", func(t *testing.T) {
		loader := &gatedReloadLoader{started: make(chan struct{}), release: make(chan struct{})}
		cfg, err := NewWithContext[any](context.Background(), WithLoaders(loader))
		require.NoError(t, err)
		beforeRevision := configRevision(cfg)

		done := make(chan error, 1)
		go func() {
			done <- cfg.ReloadWithContext(context.Background(), WithIncremental(false))
		}()
		<-loader.started
		cfg.Freeze()
		close(loader.release)

		err = <-done
		require.ErrorIs(t, err, ErrConfigFrozen)
		assert.Equal(t, beforeRevision, configRevision(cfg))
		assert.Equal(t, "before", cfg.MustGet("key"))
	})

	t.Run("extend", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"key": "before"})
		beforeRevision := configRevision(cfg)
		loader := &gatedLoader{
			source:  "gated-extend",
			data:    map[string]any{"key": "after"},
			started: make(chan struct{}),
			release: make(chan struct{}),
		}

		done := make(chan error, 1)
		go func() { done <- cfg.ExtendWithContext(context.Background(), loader) }()
		<-loader.started
		cfg.Freeze()
		close(loader.release)

		err := <-done
		require.ErrorIs(t, err, ErrConfigFrozen)
		assert.Equal(t, beforeRevision, configRevision(cfg))
		assert.Equal(t, "before", cfg.MustGet("key"))
	})
}

func TestReloadRevisionConflictRetriesWithoutDuplicateSignals(t *testing.T) {
	loader := &conflictingReloadLoader{started: make(chan struct{}), release: make(chan struct{})}
	cfg, err := NewWithContext[any](context.Background(), WithLoaders(loader))
	require.NoError(t, err)
	metrics := cfg.EnableObservability()
	events := cfg.EnableEvents()
	var reloadEvents, changeEvents int
	events.On("reload", func(_ ...any) { reloadEvents++ })
	events.On("change", func(_ ...any) { changeEvents++ })

	done := make(chan error, 1)
	go func() {
		done <- cfg.ReloadWithContext(context.Background(), WithIncremental(false))
	}()
	<-loader.started
	require.NoError(t, cfg.Set("runtime.conflict", true))
	close(loader.release)
	require.NoError(t, <-done)

	assert.Equal(t, int32(3), loader.calls.Load(), "the conflicted candidate must be rebuilt once")
	assert.Equal(t, "after-retry", cfg.MustGet("key"))
	assert.Equal(t, 1, reloadEvents)
	// Set and the committed reload each emit one change event. The superseded
	// candidate must not emit another one.
	assert.Equal(t, 2, changeEvents)
	stats := metrics.Statistics()
	assert.Equal(t, 1, stats["reload_count"])
	assert.Equal(t, 0, stats["reload_failed_count"])
}

func TestSourceTransactionLifecycleRechecks(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"key": "before"})
	var nilContext context.Context
	require.ErrorIs(t, cfg.ReloadWithContext(nilContext), ErrConfigInvalid)
	require.ErrorIs(t, cfg.ExtendWithContext(nilContext, &stubLoader{source: "unused"}), ErrConfigInvalid)
	require.ErrorIs(t, cfg.ExtendWithContext(context.Background(), nil), ErrConfigInvalid)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, cfg.ReloadWithContext(canceled), context.Canceled)
	require.ErrorIs(t, cfg.ExtendWithContext(canceled, &stubLoader{source: "unused"}), context.Canceled)

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer deadlineCancel()
	require.ErrorIs(t, cfg.ExtendWithContext(deadlineCtx, &contextProbeLoader{wait: true}), context.DeadlineExceeded)

	canceledDuringCommit, cancelDuringCommit := context.WithCancel(context.Background())
	err := cfg.runSourceTransaction(canceledDuringCommit, extendTransaction, func(_ context.Context, candidate *Config[any]) (sourceTransactionOutcome, error) {
		candidate.envConfig = map[string]any{"key": "not-published"}
		cancelDuringCommit()
		return sourceTransactionOutcome{publish: true}, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "before", cfg.MustGet("key"))

	t.Run("canceled after candidate preparation", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"key": "before"})
		loader := &nonCancelableGatedLoader{
			source:  "cancel-before-publish",
			data:    map[string]any{"key": "after"},
			started: make(chan struct{}),
			release: make(chan struct{}),
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- cfg.ExtendWithContext(ctx, loader) }()
		<-loader.started
		cancel()
		close(loader.release)
		require.ErrorIs(t, <-done, context.Canceled)
		assert.Equal(t, "before", cfg.MustGet("key"))
	})

	t.Run("closed after candidate admission", func(t *testing.T) {
		cfg := newTestConfig(t, map[string]any{"key": "before"})
		loader := &nonCancelableGatedLoader{
			source:  "close-before-publish",
			data:    map[string]any{"key": "after"},
			started: make(chan struct{}),
			release: make(chan struct{}),
		}
		done := make(chan error, 1)
		go func() { done <- cfg.ExtendWithContext(context.Background(), loader) }()
		<-loader.started
		require.NoError(t, cfg.Close())
		close(loader.release)
		require.ErrorIs(t, <-done, ErrConfigClosed)
		assert.Equal(t, "before", cfg.MustGet("key"))
	})
}

func TestPrepareExtendCandidateUsesMaterializedBaseWhenRawSnapshotIsAbsent(t *testing.T) {
	cfg := newTestConfig(t, map[string]any{"base": "retained"})
	cfg.mu.RLock()
	candidate := cfg.snapshotSourceCandidate()
	cfg.mu.RUnlock()
	candidate.unresolvedEnvConfig = nil

	outcome, err := candidate.prepareExtendCandidate(context.Background(), &stubLoader{
		source: "extension",
		data:   map[string]any{"added": true},
	})
	require.NoError(t, err)
	require.True(t, outcome.publish)
	assert.Equal(t, "retained", candidate.envConfig["base"])
	assert.Equal(t, true, candidate.envConfig["added"])
}

func TestGetAutomaticallyRecordsSuccessfulAccesses(t *testing.T) {
	cfg, err := NewWithContext[any](context.Background(), WithLoaders(&stubLoader{source: "base", data: map[string]any{"database": map[string]any{"host": "localhost"}}}))
	require.NoError(t, err)
	metrics := cfg.EnableObservability()

	_, err = cfg.Get("database.host")
	require.NoError(t, err)
	_, err = cfg.Get("database.host")
	require.NoError(t, err)
	_, err = cfg.Get("missing")
	require.Error(t, err)

	stats := metrics.Statistics()
	assert.Equal(t, 1, stats["accessed_keys"])
	top, ok := stats["top_accessed_keys"].(map[string]int)
	require.True(t, ok)
	assert.Equal(t, 2, top["database.host"])
}

func TestIncrementalReloadLoadsOnlyChangedFilesAndReusesOtherLayers(t *testing.T) {
	dir := t.TempDir()
	basePath := dir + "/base.json"
	overridePath := dir + "/override.json"
	require.NoError(t, os.WriteFile(basePath, []byte(`{"base":"v1","shared":"base"}`), 0o600))
	require.NoError(t, os.WriteFile(overridePath, []byte(`{"override":"stable","shared":"override"}`), 0o600))
	base := &countingJSONFileLoader{path: basePath}
	override := &countingJSONFileLoader{path: overridePath}
	cfg, err := NewWithContext[any](context.Background(), WithLoaders(base, override))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(basePath, []byte(`{"base":"v2","shared":"new-base"}`), 0o600))
	err = cfg.ReloadWithContext(context.Background(), WithIncremental(true))
	require.NoError(t, err)

	assert.Equal(t, 2, base.calls)
	assert.Equal(t, 1, override.calls, "unchanged override file must not be loaded again")
	assert.Equal(t, "v2", cfg.MustGet("base"))
	assert.Equal(t, "stable", cfg.MustGet("override"), "cached unchanged layer must remain present")
	assert.Equal(t, "override", cfg.MustGet("shared"), "cached layer precedence must be preserved")
}

func TestIncrementalReloadRefreshesUntrackableRemoteSources(t *testing.T) {
	remote := &scriptedLoader{source: "https://config.example.test/app.json", script: []scriptStep{
		{data: map[string]any{"version": "v1"}},
		{data: map[string]any{"version": "v2"}},
	}}
	cfg, err := NewWithContext[any](context.Background(), WithLoaders(remote))
	require.NoError(t, err)
	require.NoError(t, cfg.ReloadWithContext(context.Background(), WithIncremental(true)))
	assert.Equal(t, "v2", cfg.MustGet("version"))
}

func TestIncrementalReloadDetectsTransitiveCompositionDependency(t *testing.T) {
	dir := t.TempDir()
	topPath := dir + "/top.json"
	includedPath := dir + "/included.json"
	require.NoError(t, os.WriteFile(topPath, []byte(`{"_include":"included.json","top":true}`), 0o600))
	require.NoError(t, os.WriteFile(includedPath, []byte(`{"included":"v1"}`), 0o600))
	l := &countingJSONFileLoader{path: topPath}
	cfg, err := NewWithContext[any](context.Background(), WithLoaders(l))
	require.NoError(t, err)
	assert.Equal(t, "v1", cfg.MustGet("included"))

	require.NoError(t, os.WriteFile(includedPath, []byte(`{"included":"v2"}`), 0o600))
	require.NoError(t, cfg.ReloadWithContext(context.Background(), WithIncremental(true)))
	assert.Equal(t, 2, l.calls, "changing an _include dependency must reload its owning layer")
	assert.Equal(t, "v2", cfg.MustGet("included"))
}
