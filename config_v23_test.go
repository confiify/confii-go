package confii_test

// V-01 / V-02 / V-09 (Wave 23) — Negative tests for the structural
// invariants enforced by the post-audit remediation. Each test below
// would fail (or hit a deadlock / data race) against the pre-V-23
// implementation.

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------
// V-01 — fileTracker is included in the Phase 5/6/7 rollback closure.
// ---------------------------------------------------------------------

// V-01_a — Reload validation failure must be reproducible across
// successive Reload calls until the underlying file is fixed. Pre-V-01
// the second Reload short-circuited at the incremental gate (file
// hash matched the recorded-bad state) and silently returned nil even
// though envConfig was still pre-failure.
func TestV01_ReloadFailure_DoesNotSelfHealOnRetry(t *testing.T) {
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
	cfg, err := confii.New[Model](context.Background(),
		confii.WithLoaders(loader.NewYAML(path)),
		confii.WithValidateOnLoad(true),
	)
	require.NoError(t, err)

	// Write a payload that decodes successfully as YAML but fails the
	// validator — port = 0 is below min=1. Validation runs in Phase 5
	// of Reload and must trigger the rollback closure.
	if err := os.WriteFile(path, []byte("database:\n  port: 0\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	err1 := cfg.Reload(context.Background())
	require.Error(t, err1, "first Reload must surface the validation failure")

	// Pre-V-01 the second Reload returns nil silently because the file
	// tracker recorded the malformed file's hash on the first attempt
	// and the incremental gate now thinks "nothing changed."
	err2 := cfg.Reload(context.Background())
	require.Error(t, err2,
		"V-01: second Reload of the unchanged-bad file must still surface the validation failure (file tracker rollback)")

	// Sanity: live state did not silently flip to the bad value.
	port, _ := cfg.GetInt("database.port")
	assert.Equal(t, 5432, port,
		"V-01: live envConfig must still hold the pre-failure value after two failed Reloads")
}

// ---------------------------------------------------------------------
// V-02 — Override LIFO composability (no phantom resurrection).
// ---------------------------------------------------------------------

// V-02_a — out-of-order restore must not resurrect a popped frame's
// payload. Pre-V-02 this returned "A" because restoreB() blindly wrote
// back its captured snapshot (which was envConfig AFTER A applied,
// BEFORE B applied).
func TestV02_Override_LIFO_OutOfOrderRestore_NoPhantom(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	rA, err := cfg.Override(map[string]any{"k": "A"})
	require.NoError(t, err)
	rB, err := cfg.Override(map[string]any{"k": "B"})
	require.NoError(t, err)

	rA() // pop the lower frame; "k" must remain "B" because B is still on top
	v, err := cfg.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "B", v,
		"V-02: after restoring A out of order, k must still reflect B's payload")

	rB() // pop the remaining frame; "k" returns to base
	v2, err := cfg.Get("k")
	if err == nil {
		assert.NotEqual(t, "A", v2,
			"V-02: after both restores, k must NOT resurrect A's value (phantom)")
	}
	// "k" did not exist in the base fixture, so a missing key is the
	// correct post-state.
}

// V-02_b — restore-in-order remains correct (regression check).
func TestV02_Override_LIFO_InOrderRestore_BehavesLikePreV23(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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
		"V-02: in-order restore of top frame B must reveal lower frame A's value")

	rA()
	_, err = cfg.Get("k")
	if err == nil {
		t.Fatalf("V-02: after both restores, k must be missing — got value")
	}
}

// V-02_c — restore is idempotent.
func TestV02_Override_Restore_Idempotent(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	r, err := cfg.Override(map[string]any{"k": "A"})
	require.NoError(t, err)

	r()
	r() // second call must be a no-op, not panic or double-restore
}

// ---------------------------------------------------------------------
// V-09 — Callback panics are logged with full stack via slog.
// ---------------------------------------------------------------------

// V-09_a — a panicking OnChange callback must be logged at error level
// with the affected key, callback index, panic value, and stack trace.
// Pre-V-09 the recover guard silently discarded the panic value.
func TestV09_OnChangePanic_LoggedWithStack(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewJSONHandler(&safeWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	cfg, err := confii.New[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
		confii.WithLogger(logger),
	)
	require.NoError(t, err)

	cfg.OnChange(func(key string, oldVal, newVal any) {
		panic("V-09 sentinel: pelican")
	})

	// Set fires a change callback under the hood for "database.host"
	// (a value transition from "localhost" to "elsewhere").
	err = cfg.Set("database.host", "elsewhere")
	require.NoError(t, err,
		"Set must succeed even though a registered OnChange panics (sibling-isolation contract)")

	mu.Lock()
	logged := buf.String()
	mu.Unlock()

	if !strings.Contains(logged, "OnChange callback panic recovered") {
		t.Fatalf("V-09: expected panic-recovery log line, got:\n%s", logged)
	}
	if !strings.Contains(logged, "V-09 sentinel: pelican") {
		t.Fatalf("V-09: expected panic value in log, got:\n%s", logged)
	}
	if !strings.Contains(logged, "stack") {
		t.Fatalf("V-09: expected stack attribute in log, got:\n%s", logged)
	}
	if !strings.Contains(logged, "goroutine") {
		t.Fatalf("V-09: stack trace must contain 'goroutine' marker, got:\n%s", logged)
	}
}

// V-09_b — a panicking callback does not abort sibling callbacks.
func TestV09_OnChangePanic_SiblingsContinue(t *testing.T) {
	cfg, err := confii.New[any](context.Background(),
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
		"V-09: sibling callback after a panicking one must still fire")
}

// safeWriter serializes writes for the JSON slog handler in tests that
// inspect the buffer concurrently with logging goroutines.
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
