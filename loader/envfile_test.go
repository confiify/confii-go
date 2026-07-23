package loader

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confii "github.com/confiify/confii-go"
)

func TestEnvFileLoader_Load(t *testing.T) {
	l := NewEnvFile("testdata/simple.env")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "localhost", result["HOST"])
	assert.Equal(t, 5432, result["PORT"])
	assert.Equal(t, true, result["DEBUG"])
	assert.Equal(t, "my app", result["NAME"])       // double-quoted
	assert.Equal(t, "raw_value", result["SECRET"])  // single-quoted
	assert.Equal(t, "some_value", result["INLINE"]) // inline comment stripped

	// Nested key via dot notation.
	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "db-server", db["host"])
}

func TestEnvFileLoader_MissingFile(t *testing.T) {
	l := NewEnvFile("testdata/nonexistent.env")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnvFileLoader_DefaultPath(t *testing.T) {
	l := NewEnvFile("")
	assert.Equal(t, ".env", l.Source())
}

// writeEnvFile is a helper that writes the given content to a temp .env file
// and returns its path.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestEnvFileLoader_MalformedLinePolicies covers G31: malformed lines
// (non-blank, non-comment lines without `=`) must be surfaced according to
// the configured ErrorPolicy rather than silently dropped.
func TestEnvFileLoader_MalformedLinePolicies(t *testing.T) {
	// Line 2 ("NOEQUALS") is malformed; line 4 ("=novalueoneitherside")
	// still parses because it does contain `=`; line 5 has trailing
	// whitespace and no `=`, so it would be malformed as well, but Raise
	// stops at the first malformed line so we keep this content shape
	// for Warn/Ignore tests.
	const content = "GOOD=1\n" +
		"NOEQUALS\n" +
		"AFTER=ok\n" +
		"=novalueoneitherside\n" +
		"trailing whitespace and no equals \n"

	cases := []struct {
		name   string
		policy confii.ErrorPolicy
	}{
		{"raise", confii.ErrorPolicyRaise},
		{"warn", confii.ErrorPolicyWarn},
		{"ignore", confii.ErrorPolicyIgnore},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEnvFile(t, content)

			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			l := NewEnvFile(path,
				WithEnvFileErrorPolicy(tc.policy),
				WithEnvFileLogger(logger),
			)
			result, err := l.Load(context.Background())

			switch tc.policy {
			case confii.ErrorPolicyRaise:
				require.Error(t, err)
				// The error must be a typed ConfigError that references the file.
				var ce *confii.ConfigError
				require.True(t, errors.As(err, &ce), "expected *confii.ConfigError, got %T", err)
				assert.Equal(t, path, ce.Source)
				// The wrapped chain must include ErrConfigLoad and the line number.
				assert.True(t, errors.Is(err, confii.ErrConfigLoad))
				assert.Contains(t, err.Error(), "line 2")
				assert.Contains(t, err.Error(), "NOEQUALS")
				// On Raise, parsing stops at the first malformed line, so
				// no result map is returned.
				assert.Nil(t, result)
				// Nothing should have been warn-logged.
				assert.Empty(t, logBuf.String())

			case confii.ErrorPolicyWarn:
				require.NoError(t, err)
				require.NotNil(t, result)
				// Both malformed lines (2 and 5) should have been warn-logged.
				out := logBuf.String()
				assert.Contains(t, out, `level=WARN`)
				assert.Contains(t, out, "envfile: malformed line skipped")
				assert.Contains(t, out, "line=2")
				assert.Contains(t, out, "line=5")
				assert.Contains(t, out, path)
				// Valid lines were parsed.
				assert.Equal(t, 1, result["GOOD"])
				assert.Equal(t, "ok", result["AFTER"])
				// "=novalueoneitherside" has an empty key after TrimSpace;
				// it still parses (key "", value "novalueoneitherside")
				// because `=` is present, satisfying the audit guarantee.
				assert.Equal(t, "novalueoneitherside", result[""])

			case confii.ErrorPolicyIgnore:
				require.NoError(t, err)
				require.NotNil(t, result)
				// Silently skipped: nothing logged.
				assert.Empty(t, logBuf.String())
				assert.Equal(t, 1, result["GOOD"])
				assert.Equal(t, "ok", result["AFTER"])
			}
		})
	}
}

// TestEnvFileLoader_DefaultPolicyIsRaise verifies that, with no options,
// the loader defaults to ErrorPolicyRaise (matching defaultOptions in
// config_options.go) and surfaces malformed lines.
func TestEnvFileLoader_DefaultPolicyIsRaise(t *testing.T) {
	path := writeEnvFile(t, "OK=1\nNOEQUALS\n")

	l := NewEnvFile(path)
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
	assert.Contains(t, err.Error(), "line 2")
}

// TestEnvFileLoader_RegressionSilentDropClosed is the regression test for
// G31: the previous behavior silently dropped malformed lines under all
// policies. After the fix, only ErrorPolicyIgnore preserves that legacy
// silent-skip behavior; Raise and Warn must surface the line.
func TestEnvFileLoader_RegressionSilentDropClosed(t *testing.T) {
	path := writeEnvFile(t, "OK=1\nNOEQUALS\n")

	// Raise must NOT silently drop.
	_, err := NewEnvFile(path, WithEnvFileErrorPolicy(confii.ErrorPolicyRaise)).
		Load(context.Background())
	require.Error(t, err, "Raise must not silently drop malformed lines (G31)")

	// Warn must NOT silently drop: it must produce a log record.
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	_, err = NewEnvFile(path,
		WithEnvFileErrorPolicy(confii.ErrorPolicyWarn),
		WithEnvFileLogger(logger),
	).Load(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, logBuf.String(), "Warn must emit a log record for malformed lines (G31)")
	assert.True(t, strings.Contains(logBuf.String(), "malformed line skipped"))

	// Ignore preserves legacy silent-skip semantics for callers who opt in.
	logBuf.Reset()
	_, err = NewEnvFile(path,
		WithEnvFileErrorPolicy(confii.ErrorPolicyIgnore),
		WithEnvFileLogger(logger),
	).Load(context.Background())
	require.NoError(t, err)
	assert.Empty(t, logBuf.String())
}

// TestEnvFileLoader_NilLoggerIgnored ensures WithEnvFileLogger(nil) is a
// no-op rather than panicking when a Warn policy fires.
func TestEnvFileLoader_NilLoggerIgnored(t *testing.T) {
	path := writeEnvFile(t, "NOEQUALS\n")
	l := NewEnvFile(path,
		WithEnvFileErrorPolicy(confii.ErrorPolicyWarn),
		WithEnvFileLogger(nil),
	)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
}
