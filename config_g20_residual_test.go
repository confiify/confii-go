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

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Wave 21 G20-residual: startWatching must filter non-file Loader sources at
// the call site BEFORE handing them to watch.New.
//
// Wave 20 Fixer-AT added a defensive scheme allowlist inside watch.New so
// non-file sources are skipped with a "watch: skipping non-file source"
// warning. That made the residual a WASTE issue (noisy warning log every
// call) rather than a CORRECTNESS bug. Wave 21 Fixer-AV closes the residual
// by pre-filtering in startWatching: envPrefixAutoLoader / HTTPLoader /
// cloud-store loader sources never reach watch.New, so the safety-net
// warning is silent on the happy path.
// =============================================================================

// safeBuffer is a goroutine-safe wrapper around bytes.Buffer used as a
// destination for slog handlers in these tests. The watcher logs from a
// background goroutine while the test goroutine reads buf.String(); both
// ends must be serialized to keep -race happy.
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

// captureLogger returns a slog.Logger that writes JSON to a goroutine-safe
// buffer plus the buffer for assertion. Using JSON keeps assertions stable
// across slog formatting tweaks.
func captureLogger() (*slog.Logger, *safeBuffer) {
	buf := &safeBuffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), buf
}

// TestG20Residual_StartWatching_FiltersNonFileSources observable: when a
// Config is constructed with a mix of file and non-file loaders under
// WithDynamicReloading(true), the watch package's "skipping non-file
// source" warning MUST NOT appear in the captured log output. Pre-Wave 21
// startWatching forwarded every loader.Source() (including
// "environment:APP" and "http(s)://...") to watch.New, which then logged
// each at warn level. Post-fix the call site filters those sources before
// watch.New ever sees them.
func TestG20Residual_StartWatching_FiltersNonFileSources(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "real.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("k: v\n"), 0644))

	logger, buf := captureLogger()

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(
			loader.NewYAML(yamlPath),
			loader.NewHTTP("http://127.0.0.1:1/never-reachable.yaml"),
		),
		// Adds an envPrefixAutoLoader with Source() == "environment:APP",
		// which is a non-file source.
		confii.WithEnvPrefix("APP"),
		confii.WithLogger(logger),
		confii.WithDynamicReloading(true),
		// HTTPLoader will fail at load time; tolerate it as warn so New()
		// still constructs the Config and reaches startWatching.
		confii.WithOnError(confii.ErrorPolicyWarn),
	)
	require.NoError(t, err)
	defer cfg.StopWatching()

	out := buf.String()

	// Post-fix observable: the watch package's safety-net warning string
	// MUST NOT appear because non-file sources were filtered out at the
	// call site before watch.New saw them. Pre-fix this exact substring
	// appeared once per non-file loader, every time startWatching ran.
	assert.NotContains(t, out, "skipping non-file source",
		"startWatching must pre-filter non-file sources so the watch package "+
			"never logs its 'skipping non-file source' safety-net warning "+
			"on the happy path")

	// Tighter observable: scan WARN records for the watch-package
	// message specifically. Other WARN records (e.g. the HTTPLoader's
	// "loader error" from the unreachable URL) are unrelated to the
	// startWatching pre-filter contract and must not influence this
	// assertion.
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, `"level":"WARN"`) {
			continue
		}
		assert.NotContains(t, line, "skipping non-file source",
			"watch package safety-net warning must not appear when "+
				"startWatching has already filtered non-file sources")
	}
}

// TestG20Residual_StartWatching_FileSources_StillWatched observable:
// filtering must NOT break the happy path. Mutating a watched YAML file
// still fires the reload pipeline.
func TestG20Residual_StartWatching_FileSources_StillWatched(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "watched.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("k: before\n"), 0644))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML(yamlPath)),
		confii.WithEnvPrefix("APP"), // non-file source mixed in
		confii.WithDynamicReloading(true),
	)
	require.NoError(t, err)
	defer cfg.StopWatching()

	v, err := cfg.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "before", v)

	// Atomic-save: write tmpfile then rename over target. This pattern
	// is the editor-canonical "safe write" sequence and is the one the
	// Wave 20 watcher fix exists to handle.
	tmp := filepath.Join(dir, "watched.yaml.tmp")
	require.NoError(t, os.WriteFile(tmp, []byte("k: after\n"), 0644))
	require.NoError(t, os.Rename(tmp, yamlPath))

	// Poll for the reload-applied observable. Reload runs asynchronously
	// from the watcher goroutine, so wait until cfg.Get reflects the
	// new value or the deadline expires.
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
