package loader

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		opts    []YAMLOption
		wantNil bool
		wantErr bool
		errType error
	}{
		{
			name: "valid yaml",
			path: "testdata/simple.yaml",
		},
		{
			// G07: post-fix, the default policy is Raise, so a missing
			// file is a typed ConfigError — not (nil, nil). Callers who
			// want the old graceful-absence behavior must opt in.
			name:    "missing file under default policy raises",
			path:    "testdata/nonexistent.yaml",
			wantErr: true,
			errType: confii.ErrConfigLoad,
		},
		{
			name:    "missing file with WithYAMLErrorPolicy(Ignore) returns nil",
			path:    "testdata/nonexistent.yaml",
			opts:    []YAMLOption{WithYAMLErrorPolicy(confii.ErrorPolicyIgnore)},
			wantNil: true,
		},
		{
			name:    "invalid yaml returns format error",
			path:    "testdata/invalid.yaml",
			wantErr: true,
			errType: confii.ErrConfigFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewYAML(tt.path, tt.opts...)
			assert.Equal(t, tt.path, l.Source())

			result, err := l.Load(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.True(t, errors.Is(err, tt.errType), "expected %v, got %v", tt.errType, err)
				}
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, result)
				return
			}

			// Verify content.
			db, ok := result["database"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "localhost", db["host"])
			assert.Equal(t, 5432, db["port"])
		})
	}
}

// TestYAMLLoader_MissingFile_Policies covers G07: a missing source file
// must be surfaced according to the configured ErrorPolicy rather than
// silently returning (nil, nil) regardless of policy.
func TestYAMLLoader_MissingFile_Policies(t *testing.T) {
	missing := "testdata/nonexistent.yaml"

	t.Run("raise", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewYAML(missing,
			WithYAMLErrorPolicy(confii.ErrorPolicyRaise),
			WithYAMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.Error(t, err)
		assert.Nil(t, result)
		var ce *confii.ConfigError
		require.True(t, errors.As(err, &ce), "expected *confii.ConfigError, got %T", err)
		assert.Equal(t, missing, ce.Source)
		assert.True(t, errors.Is(err, confii.ErrConfigLoad))
		// Raise must not also produce a warn log record.
		assert.Empty(t, logBuf.String())
	})

	t.Run("warn", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewYAML(missing,
			WithYAMLErrorPolicy(confii.ErrorPolicyWarn),
			WithYAMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		out := logBuf.String()
		assert.Contains(t, out, "level=WARN")
		assert.Contains(t, out, "yaml: source file missing")
		assert.Contains(t, out, missing)
	})

	t.Run("ignore", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		l := NewYAML(missing,
			WithYAMLErrorPolicy(confii.ErrorPolicyIgnore),
			WithYAMLLogger(logger),
		)
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Nil(t, result)
		// Ignore must be silent (distinct from Warn, G07).
		assert.Empty(t, logBuf.String())
	})
}

// TestYAMLLoader_DefaultPolicyIsRaise verifies that, with no options,
// the loader defaults to ErrorPolicyRaise and surfaces a missing file
// as a typed error rather than (nil, nil).
func TestYAMLLoader_DefaultPolicyIsRaise(t *testing.T) {
	l := NewYAML("testdata/nonexistent.yaml")
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}

// TestYAMLLoader_NilLoggerIgnored ensures WithYAMLLogger(nil) is a no-op
// rather than panicking when a Warn policy fires.
func TestYAMLLoader_NilLoggerIgnored(t *testing.T) {
	l := NewYAML("testdata/nonexistent.yaml",
		WithYAMLErrorPolicy(confii.ErrorPolicyWarn),
		WithYAMLLogger(nil),
	)
	_, err := l.Load(context.Background())
	require.NoError(t, err)
}

// TestYAMLLoader_NormalizesNonStringKeys covers D01: a YAML mapping
// whose nested map uses non-string keys (integers, in this case) must
// be normalized to map[string]any with stringified keys, not left as
// the gopkg.in/yaml.v3-native map[interface{}]interface{} (which
// breaks dot-path access, JSON export, and mapstructure decoding).
func TestYAMLLoader_NormalizesNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ports.yaml")
	body := "" +
		"ports:\n" +
		"  80: http\n" +
		"  443: https\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	ports, ok := result["ports"].(map[string]any)
	require.True(t, ok, "expected ports to be map[string]any, got %T", result["ports"])
	assert.Equal(t, "http", ports["80"])
	assert.Equal(t, "https", ports["443"])
}

// TestYAMLLoader_NormalizesNestedNonStringKeys verifies the
// normalization recurses through multiple levels of nesting so that
// no map[interface{}]interface{} sub-tree leaks out of the loader
// even when only a deeply-nested map contains non-string keys.
func TestYAMLLoader_NormalizesNestedNonStringKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested.yaml")
	body := "" +
		"services:\n" +
		"  web:\n" +
		"    ports:\n" +
		"      80: http\n" +
		"      443: https\n" +
		"    flags:\n" +
		"      true: enabled\n" +
		"      false: disabled\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	services, ok := result["services"].(map[string]any)
	require.True(t, ok, "services should be map[string]any, got %T", result["services"])
	web, ok := services["web"].(map[string]any)
	require.True(t, ok, "services.web should be map[string]any, got %T", services["web"])
	ports, ok := web["ports"].(map[string]any)
	require.True(t, ok, "services.web.ports should be map[string]any, got %T", web["ports"])
	assert.Equal(t, "http", ports["80"])
	assert.Equal(t, "https", ports["443"])
	flags, ok := web["flags"].(map[string]any)
	require.True(t, ok, "services.web.flags should be map[string]any, got %T", web["flags"])
	assert.Equal(t, "enabled", flags["true"])
	assert.Equal(t, "disabled", flags["false"])
}

// TestYAMLLoader_StringKeysUnchanged is the regression baseline for
// D01: ordinary string-keyed YAML must continue to decode with values
// of their natural Go types. The normalization pass must not rewrap
// scalars or change their types.
func TestYAMLLoader_StringKeysUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.yaml")
	body := "" +
		"database:\n" +
		"  host: localhost\n" +
		"  port: 5432\n" +
		"  ssl: true\n" +
		"tags:\n" +
		"  - alpha\n" +
		"  - beta\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, 5432, db["port"])
	assert.Equal(t, true, db["ssl"])

	tags, ok := result["tags"].([]any)
	require.True(t, ok, "tags should be []any, got %T", result["tags"])
	assert.Equal(t, []any{"alpha", "beta"}, tags)
}

// TestYAMLLoader_SliceWithMapElements covers the normalization
// recursion through slices: a sequence whose elements are mappings
// with non-string keys must yield []any of map[string]any after
// normalization.
func TestYAMLLoader_SliceWithMapElements(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "list.yaml")
	body := "" +
		"items:\n" +
		"  - 1: one\n" +
		"    2: two\n" +
		"  - 3: three\n" +
		"    4: four\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	result, err := NewYAML(path).Load(context.Background())
	require.NoError(t, err)

	items, ok := result["items"].([]any)
	require.True(t, ok, "items should be []any, got %T", result["items"])
	require.Len(t, items, 2)

	first, ok := items[0].(map[string]any)
	require.True(t, ok, "items[0] should be map[string]any, got %T", items[0])
	assert.Equal(t, "one", first["1"])
	assert.Equal(t, "two", first["2"])

	second, ok := items[1].(map[string]any)
	require.True(t, ok, "items[1] should be map[string]any, got %T", items[1])
	assert.Equal(t, "three", second["3"])
	assert.Equal(t, "four", second["4"])
}

// TestYAMLLoader_PresentFileUnaffectedByPolicy confirms the policy only
// affects error paths; a present, valid file loads identically under
// every policy.
func TestYAMLLoader_PresentFileUnaffectedByPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: 1\n"), 0o600))

	for _, p := range []confii.ErrorPolicy{
		confii.ErrorPolicyRaise,
		confii.ErrorPolicyWarn,
		confii.ErrorPolicyIgnore,
	} {
		l := NewYAML(path, WithYAMLErrorPolicy(p))
		result, err := l.Load(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, result["a"])
	}
}
