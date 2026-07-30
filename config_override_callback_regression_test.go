// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReloadFailure_DoesNotSelfHealOnRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(path, []byte("database:\n  port: 5432\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	type Model struct {
		Database struct {
			Port int `validate:"min=1,max=65535"`
		}
	}
	cfg, err := confii.NewWithContext[Model](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithValidateOnLoad(true),
	)
	require.NoError(t, err)

	if err := os.WriteFile(path, []byte("database:\n  port: 0\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	err1 := cfg.ReloadWithContext(context.Background())
	require.Error(t, err1, "first Reload must surface the validation failure")

	err2 := cfg.ReloadWithContext(context.Background())
	require.Error(t, err2,
		":second Reload of the unchanged-bad file must still surface the validation failure (file tracker rollback)")

	port, _ := cfg.GetInt("database.port")
	assert.Equal(t, 5432, port,
		":live envConfig must still hold the pre-failure value after two failed Reloads")
}

func TestOverride_LIFO_OutOfOrderRestore_NoPhantom(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	rA, err := cfg.Override(map[string]any{"k": "A"})
	require.NoError(t, err)
	rB, err := cfg.Override(map[string]any{"k": "B"})
	require.NoError(t, err)

	rA()
	v, err := cfg.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "B", v,
		":after restoring A out of order, k must still reflect B's payload")

	rB()
	v2, err := cfg.Get("k")
	if err == nil {
		assert.NotEqual(t, "A", v2,
			":after both restores, k must NOT resurrect A's value (phantom)")
	}

}

func TestOverride_LIFO_InOrderRestore_BehavesLikePreV23(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	rA, err := cfg.Override(map[string]any{"k": "A"})
	require.NoError(t, err)
	rB, err := cfg.Override(map[string]any{"k": "B"})
	require.NoError(t, err)

	rB()
	v, _ := cfg.Get("k")
	assert.Equal(t, "A", v,
		":in-order restore of top frame B must reveal lower frame A's value")

	rA()
	_, err = cfg.Get("k")
	if err == nil {
		t.Fatalf(":after both restores, k must be missing — got value")
	}
}

func TestOverride_Restore_Idempotent(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	r, err := cfg.Override(map[string]any{"k": "A"})
	require.NoError(t, err)

	r()
	r()
}

func TestOnChangePanic_LoggedWithStack(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewJSONHandler(&safeWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithLogger(logger),
	)
	require.NoError(t, err)

	cfg.OnChange(func(key string, oldVal, newVal any) {
		panic(" sentinel:pelican")
	})

	err = cfg.Set("database.host", "elsewhere")
	require.NoError(t, err,
		"Set must succeed even though a registered OnChange panics (sibling-isolation contract)")

	mu.Lock()
	logged := buf.String()
	mu.Unlock()

	if !strings.Contains(logged, "OnChange callback panic recovered") {
		t.Fatalf(":expected panic-recovery log line, got:\n%s", logged)
	}
	if !strings.Contains(logged, " sentinel:pelican") {
		t.Fatalf(":expected panic value in log, got:\n%s", logged)
	}
	if !strings.Contains(logged, "stack") {
		t.Fatalf(":expected stack attribute in log, got:\n%s", logged)
	}
	if !strings.Contains(logged, "goroutine") {
		t.Fatalf(":stack trace must contain 'goroutine' marker, got:\n%s", logged)
	}
}

func TestOnChangePanic_SiblingsContinue(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithLogger(discardLogger()),
	)
	require.NoError(t, err)

	var siblingFired bool
	var mu sync.Mutex
	cfg.OnChange(func(key string, oldVal, newVal any) {
		panic("first callback panics")
	})
	cfg.OnChange(func(key string, oldVal, newVal any) {
		mu.Lock()
		defer mu.Unlock()
		siblingFired = true
	})

	require.NoError(t, cfg.Set("database.host", "elsewhere"))

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, siblingFired,
		":sibling callback after a panicking one must still fire")
}

type safeWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
