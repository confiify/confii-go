package confii

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/confiify/confii-go/selfconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// G07: Error policy semantics
//
// These tests pin the post-fix contract: the three ErrorPolicy values are
// strictly distinct (Raise raises, Warn logs, Ignore is silent). They cover
// loader errors, composition errors, and self-config on_error parsing.
// =========================================================================

// captureLoggerOpts builds a logger that writes warn-level records to the
// returned buffer so test assertions can inspect them.
func captureLoggerOpts() (*bytes.Buffer, []Option) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return buf, []Option{WithLogger(logger)}
}

// -------------------------------------------------------------------------
// Loader-error path: Ignore vs Warn must differ.
// -------------------------------------------------------------------------

// TestConfig_LoaderError_RaisePolicy_Surfaces verifies that under the
// default ErrorPolicyRaise a loader failure aborts New.
func TestConfig_LoaderError_RaisePolicy_Surfaces(t *testing.T) {
	bad := &stubLoader{source: "bad", err: errors.New("boom")}
	_, err := New[any](context.Background(),
		WithLoaders(bad),
		WithOnError(ErrorPolicyRaise),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// TestConfig_LoaderError_WarnPolicy_LogsAndContinues verifies that under
// ErrorPolicyWarn a loader failure is logged at warn level and the
// remaining sources still produce a Config.
func TestConfig_LoaderError_WarnPolicy_LogsAndContinues(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()
	bad := &stubLoader{source: "bad", err: errors.New("warn-me")}
	good := &stubLoader{source: "good", data: map[string]any{"k": "v"}}

	opts := append([]Option{
		WithLoaders(bad, good),
		WithOnError(ErrorPolicyWarn),
	}, logOpts...)

	cfg, err := New[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	got, _ := cfg.Get("k")
	assert.Equal(t, "v", got)

	out := logBuf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "loader error")
	assert.Contains(t, out, "warn-me")
}

// TestConfig_LoaderError_IgnorePolicy_SilentlyContinues is the regression
// test pinning the Ignore-vs-Warn distinction (G07): Ignore must produce
// NO log record, whereas Warn always does. Pre-fix the two policies were
// indistinguishable — both warn-logged.
func TestConfig_LoaderError_IgnorePolicy_SilentlyContinues(t *testing.T) {
	logBuf, logOpts := captureLoggerOpts()
	bad := &stubLoader{source: "bad", err: errors.New("hush")}
	good := &stubLoader{source: "good", data: map[string]any{"k": "v"}}

	opts := append([]Option{
		WithLoaders(bad, good),
		WithOnError(ErrorPolicyIgnore),
	}, logOpts...)

	cfg, err := New[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	got, _ := cfg.Get("k")
	assert.Equal(t, "v", got)

	// G07 contract: Ignore is silent. No log record about the loader
	// failure should appear in the buffer.
	assert.NotContains(t, logBuf.String(), "loader error",
		"ErrorPolicyIgnore must not produce a 'loader error' log record (G07)")
	assert.NotContains(t, logBuf.String(), "hush",
		"ErrorPolicyIgnore must not leak the underlying error message (G07)")
}

// -------------------------------------------------------------------------
// Composition-error path: previously always swallowed.
// -------------------------------------------------------------------------

// writeYAMLFile is a small helper to write YAML content to a fresh temp file.
func writeYAMLFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// composeFailingYAML produces a YAML payload whose `_include` references a
// non-existent file. The composer surfaces this as an error; pre-G07 the
// error was logged and the raw (un-composed) data was loaded regardless of
// policy.
const composeFailingYAML = "" +
	"_include:\n" +
	"  - this_file_does_not_exist_for_g07.yaml\n" +
	"app:\n" +
	"  name: raw\n"

// TestConfig_CompositionError_RaisePolicy_SurfacesError verifies the
// composer-error path under ErrorPolicyRaise. Pre-G07 this returned a
// Config with un-composed data; post-G07 it must abort New.
func TestConfig_CompositionError_RaisePolicy_SurfacesError(t *testing.T) {
	path := writeYAMLFile(t, composeFailingYAML)
	_, err := New[any](context.Background(),
		WithLoaders(&fileAutoLoader{path: path}),
		WithOnError(ErrorPolicyRaise),
	)
	require.Error(t, err, "composer error must surface under Raise (G07)")
}

// TestConfig_CompositionError_WarnPolicy_LogsAndContinues verifies that
// under ErrorPolicyWarn a composer error is logged and the raw data is
// still loaded (the legacy fallback path), preserving best-effort behavior
// for callers who opt in.
func TestConfig_CompositionError_WarnPolicy_LogsAndContinues(t *testing.T) {
	path := writeYAMLFile(t, composeFailingYAML)
	logBuf, logOpts := captureLoggerOpts()
	opts := append([]Option{
		WithLoaders(&fileAutoLoader{path: path}),
		WithOnError(ErrorPolicyWarn),
	}, logOpts...)

	cfg, err := New[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// Raw data was kept so the un-composed `app.name` survives.
	got, _ := cfg.Get("app.name")
	assert.Equal(t, "raw", got)

	out := logBuf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "composition error")
}

// TestConfig_CompositionError_IgnorePolicy_SilentlyContinues verifies the
// distinction from Warn: Ignore must not produce a 'composition error'
// log record.
func TestConfig_CompositionError_IgnorePolicy_SilentlyContinues(t *testing.T) {
	path := writeYAMLFile(t, composeFailingYAML)
	logBuf, logOpts := captureLoggerOpts()
	opts := append([]Option{
		WithLoaders(&fileAutoLoader{path: path}),
		WithOnError(ErrorPolicyIgnore),
	}, logOpts...)

	cfg, err := New[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// Raw data is still loaded so the caller still gets *something*.
	got, _ := cfg.Get("app.name")
	assert.Equal(t, "raw", got)

	assert.NotContains(t, logBuf.String(), "composition error",
		"ErrorPolicyIgnore must not produce a 'composition error' log record (G07)")
}

// -------------------------------------------------------------------------
// Self-config on_error validation.
// -------------------------------------------------------------------------

// TestParseErrorPolicy_AcceptsValidStrings pins the recognized values.
func TestParseErrorPolicy_AcceptsValidStrings(t *testing.T) {
	cases := map[string]ErrorPolicy{
		"raise":   ErrorPolicyRaise,
		"warn":    ErrorPolicyWarn,
		"ignore":  ErrorPolicyIgnore,
		"RAISE":   ErrorPolicyRaise, // case-insensitive
		"  Warn ": ErrorPolicyWarn,  // whitespace trimmed
	}
	for in, want := range cases {
		got, err := ParseErrorPolicy(in)
		require.NoError(t, err, "input %q", in)
		assert.Equal(t, want, got, "input %q", in)
	}
}

// TestParseErrorPolicy_RejectsUnknownStrings verifies the typed-error
// behavior for invalid policy strings.
func TestParseErrorPolicy_RejectsUnknownStrings(t *testing.T) {
	cases := []string{"", "warning", "fatal", "yes", "RAIS"}
	for _, in := range cases {
		_, err := ParseErrorPolicy(in)
		require.Error(t, err, "input %q should be rejected", in)
		var ce *ConfigError
		require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
		assert.True(t, errors.Is(err, ErrConfigLoad))
		// The error message should reference the bad input.
		assert.Contains(t, err.Error(), fmt.Sprintf("%q", in))
	}
}

// withTempCWD changes into a fresh temp directory for the test (and
// restores the original CWD at cleanup), and clears the selfconfig
// cache so a fresh Read picks up the temp file. Returns the temp dir.
func withTempCWD(t *testing.T) string {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(orig)
		selfconfig.ClearCache()
	})
	selfconfig.ClearCache()
	return dir
}

// TestSelfConfig_InvalidOnErrorString_RejectedWithTypedError verifies G07's
// invalid-on_error rejection path: an unknown string in confii.yaml's
// on_error field must surface a typed *ConfigError from confii.New rather
// than being silently coerced to warn-or-ignore behavior.
func TestSelfConfig_InvalidOnErrorString_RejectedWithTypedError(t *testing.T) {
	dir := withTempCWD(t)
	yamlPath := filepath.Join(dir, "confii.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("on_error: warning\n"), 0o600))

	_, err := New[any](context.Background())
	require.Error(t, err, "invalid on_error string must be rejected (G07)")
	var ce *ConfigError
	require.True(t, errors.As(err, &ce), "expected *ConfigError, got %T", err)
	assert.True(t, errors.Is(err, ErrConfigLoad))
	assert.Contains(t, err.Error(), "warning")
}

// TestSelfConfig_ValidOnErrorString_Accepted verifies the happy path: a
// valid policy string in self-config is honored.
func TestSelfConfig_ValidOnErrorString_Accepted(t *testing.T) {
	dir := withTempCWD(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte("on_error: warn\n"), 0o600))

	// Use a logger we can inspect — a loader error under the self-config
	// supplied policy must produce a warn record (proving Warn semantics
	// were applied) rather than the default Raise.
	logBuf, logOpts := captureLoggerOpts()
	opts := append([]Option{
		WithLoaders(&stubLoader{source: "bad", err: errors.New("boom")}),
	}, logOpts...)

	cfg, err := New[any](context.Background(), opts...)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, strings.Contains(logBuf.String(), "loader error"),
		"self-config on_error=warn should activate Warn semantics")
}

// TestSelfConfig_ExplicitOptionTrumpsSelfConfigOnError documents the
// existing priority semantics: when the caller explicitly passes
// WithOnError, applySelfConfig skips the on_error field altogether
// (priority: explicit > self-config). The validator therefore does not
// fire on a malformed self-config value when the caller has supplied an
// explicit override — they get the explicit policy unchanged. This is
// intentional and consistent with how every other isSet-guarded option
// behaves.
func TestSelfConfig_ExplicitOptionTrumpsSelfConfigOnError(t *testing.T) {
	dir := withTempCWD(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "confii.yaml"),
		[]byte("on_error: warning\n"), 0o600))

	// Explicit option wins; the malformed self-config value is ignored
	// (because applySelfConfig only consults on_error when isSet is
	// false). This avoids penalising callers for a stale shared
	// self-config file when they have already pinned the policy.
	cfg, err := New[any](context.Background(),
		WithLoaders(&stubLoader{source: "ok", data: map[string]any{"k": "v"}}),
		WithOnError(ErrorPolicyRaise),
	)
	require.NoError(t, err)
	require.NotNil(t, cfg)
}
