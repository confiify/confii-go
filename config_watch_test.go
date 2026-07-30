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
	"time"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func captureLogger() (*slog.Logger, *safeBuffer) {
	buf := &safeBuffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), buf
}

func TestStartWatching_FiltersNonFileSources(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "real.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("k: v\n"), 0644))

	logger, buf := captureLogger()

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath),
			loader.NewHTTP("http://127.0.0.1:1/never-reachable.yaml"),
		),

		confii.WithEnvPrefix("APP"),
		confii.WithLogger(logger),
		confii.WithDynamicReloading(true),

		confii.WithOnError(confii.ErrorPolicyWarn),
	)
	require.NoError(t, err)
	defer cfg.StopWatching()

	out := buf.String()

	assert.NotContains(t, out, "skipping non-file source",
		"startWatching must pre-filter non-file sources so the watch package "+
			"never logs its 'skipping non-file source' safety-net warning "+
			"on the happy path")

	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, `"level":"WARN"`) {
			continue
		}
		assert.NotContains(t, line, "skipping non-file source",
			"watch package safety-net warning must not appear when "+
				"startWatching has already filtered non-file sources")
	}
}

func TestStartWatching_FileSources_StillWatched(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "watched.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("k: before\n"), 0644))

	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath)),
		confii.WithEnvPrefix("APP"),
		confii.WithDynamicReloading(true),
	)
	require.NoError(t, err)
	defer cfg.StopWatching()

	v, err := cfg.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "before", v)

	tmp := filepath.Join(dir, "watched.yaml.tmp")
	require.NoError(t, os.WriteFile(tmp, []byte("k: after\n"), 0644))
	require.NoError(t, os.Rename(tmp, yamlPath))

	deadline := time.Now().Add(3 * time.Second)
	var got any
	for time.Now().Before(deadline) {
		got, err = cfg.Get("k")
		if err == nil && got == "after" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, err)
	assert.Equal(t, "after", got, "watcher must fire reload for real file sources after filtering")
}
