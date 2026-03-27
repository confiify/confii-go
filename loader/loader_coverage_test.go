package loader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/internal/formatparse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// EnvFileLoader edge cases
// ---------------------------------------------------------------------------

func TestEnvFileLoader_CommentsAndEmptyLines(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "test.env")
	content := `# Full line comment

KEY1=value1
  # Indented comment
KEY2=value2

# Another comment
`
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "value1", result["KEY1"])
	assert.Equal(t, "value2", result["KEY2"])
}

func TestEnvFileLoader_NestedKeysViaDots(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "nested.env")
	content := `app.server.host=myhost
app.server.port=8080
app.debug=true
`
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	app, ok := result["app"].(map[string]any)
	require.True(t, ok)
	server, ok := app["server"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "myhost", server["host"])
	assert.Equal(t, 8080, server["port"])
	assert.Equal(t, true, app["debug"])
}

func TestEnvFileLoader_EscapeSequencesInDoubleQuotes(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "escape.env")
	content := `MSG="hello\nworld"
TAB="col1\tcol2"
`
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "hello\nworld", result["MSG"])
	assert.Equal(t, "col1\tcol2", result["TAB"])
}

func TestEnvFileLoader_SingleQuotesLiteral(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "single.env")
	content := `KEY='value with spaces and $pecial chars'
`
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value with spaces and $pecial chars", result["KEY"])
}

func TestEnvFileLoader_InlineCommentStripping(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "inline.env")
	content := `KEY=value # this is a comment
`
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "value", result["KEY"])
}

func TestEnvFileLoader_LineWithoutEquals(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "noequals.env")
	content := `VALID=yes
INVALID_LINE_NO_EQUALS
ALSO_VALID=true
`
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "yes", result["VALID"])
	assert.Equal(t, true, result["ALSO_VALID"])
	// INVALID_LINE_NO_EQUALS should be skipped.
	_, hasInvalid := result["INVALID_LINE_NO_EQUALS"]
	assert.False(t, hasInvalid)
}

func TestEnvFileLoader_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "empty.env")
	require.NoError(t, os.WriteFile(envFile, []byte(""), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEnvFileLoader_OnlyComments(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "comments.env")
	content := `# Comment 1
# Comment 2
# Comment 3
`
	require.NoError(t, os.WriteFile(envFile, []byte(content), 0644))

	l := NewEnvFile(envFile)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// EnvironmentLoader edge cases
// ---------------------------------------------------------------------------

func TestEnvironmentLoader_CustomSeparator_TripleDots(t *testing.T) {
	t.Setenv("XAPP_DB___HOST", "triplehost")

	l := NewEnvironment("XAPP", WithSeparator("___"))
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["db"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "triplehost", db["host"])
}

func TestEnvironmentLoader_LowercaseConversion(t *testing.T) {
	t.Setenv("LCAPP_MY_KEY", "lowered")

	l := NewEnvironment("LCAPP", WithSeparator("_"))
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	// "MY" and "KEY" should be lowercased.
	my, ok := result["my"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "lowered", my["key"])
}

// ---------------------------------------------------------------------------
// HTTPLoader edge cases
// ---------------------------------------------------------------------------

func TestHTTPLoader_WithBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"auth": "ok"}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL, WithBasicAuth("admin", "secret"))
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ok", result["auth"])
}

func TestHTTPLoader_WithBasicAuth_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"auth": "ok"}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL, WithBasicAuth("wrong", "creds"))
	_, err := l.Load(context.Background())
	assert.Error(t, err)
}

func TestHTTPLoader_WithTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key": "value"}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL, WithTimeout(50*time.Millisecond))
	_, err := l.Load(context.Background())
	assert.Error(t, err)
}

func TestHTTPLoader_ContentTypeDetection_YAML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("key: yaml-value\n"))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "yaml-value", result["key"])
}

func TestHTTPLoader_ContentTypeDetection_FallbackToExtension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Type header, but URL ends in .yaml -- however httptest
		// doesn't include path in URL matching, so it defaults to JSON.
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(`{"key": "json-fallback"}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "json-fallback", result["key"])
}

func TestHTTPLoader_CustomHeaders_Multiple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token123" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get("Accept") != "application/json" {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": "authorized"}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL, WithHeaders(map[string]string{
		"Authorization": "Bearer token123",
		"Accept":        "application/json",
	}))
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "authorized", result["status"])
}

func TestHTTPLoader_InvalidURL(t *testing.T) {
	l := NewHTTP("http://localhost:99999/nonexistent")
	_, err := l.Load(context.Background())
	assert.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}

func TestHTTPLoader_Source(t *testing.T) {
	l := NewHTTP("http://example.com/config.json")
	assert.Equal(t, "http://example.com/config.json", l.Source())
}

// ---------------------------------------------------------------------------
// INI loader edge cases
// ---------------------------------------------------------------------------

func TestINILoader_MultipleSections(t *testing.T) {
	l := NewINI("testdata/simple.ini")
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	// Check both sections exist.
	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "localhost", db["host"])
	assert.Equal(t, 5432, db["port"])
	assert.Equal(t, "mydb", db["name"])

	app, ok := result["app"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, app["debug"])
}

func TestINILoader_Source(t *testing.T) {
	l := NewINI("testdata/simple.ini")
	assert.Equal(t, "testdata/simple.ini", l.Source())
}

func TestINILoader_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.ini")
	// Write something that's not valid INI (binary-like content).
	require.NoError(t, os.WriteFile(path, []byte("\x00\x01\x02"), 0644))

	l := NewINI(path)
	// gopkg.in/ini.v1 is quite tolerant, so this may or may not error.
	// The important thing is it doesn't panic.
	_, _ = l.Load(context.Background())
}

// ---------------------------------------------------------------------------
// TOML loader edge cases
// ---------------------------------------------------------------------------

func TestTOMLLoader_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	require.NoError(t, os.WriteFile(path, []byte("[[invalid toml\n"), 0644))

	l := NewTOML(path)
	_, err := l.Load(context.Background())
	assert.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigFormat))
}

func TestTOMLLoader_Source(t *testing.T) {
	l := NewTOML("some/path.toml")
	assert.Equal(t, "some/path.toml", l.Source())
}

// ---------------------------------------------------------------------------
// YAML loader edge cases
// ---------------------------------------------------------------------------

func TestYAMLLoader_Source(t *testing.T) {
	l := NewYAML("some/path.yaml")
	assert.Equal(t, "some/path.yaml", l.Source())
}

// ---------------------------------------------------------------------------
// JSON loader edge cases
// ---------------------------------------------------------------------------

func TestJSONLoader_Source(t *testing.T) {
	l := NewJSON("some/path.json")
	assert.Equal(t, "some/path.json", l.Source())
}

// ---------------------------------------------------------------------------
// ParseContent (used by HTTP and cloud loaders)
// ---------------------------------------------------------------------------

func TestParseContent_JSON(t *testing.T) {
	data := []byte(`{"key": "value"}`)
	result, err := ParseContent(data, formatparse.FormatJSON, "test.json")
	require.NoError(t, err)
	assert.Equal(t, "value", result["key"])
}

func TestParseContent_YAML(t *testing.T) {
	data := []byte("key: value\n")
	result, err := ParseContent(data, formatparse.FormatYAML, "test.yaml")
	require.NoError(t, err)
	assert.Equal(t, "value", result["key"])
}

func TestParseContent_UnknownFormat_FallsBackToJSON(t *testing.T) {
	data := []byte(`{"key": "value"}`)
	result, err := ParseContent(data, formatparse.FormatUnknown, "test.unknown")
	require.NoError(t, err)
	assert.Equal(t, "value", result["key"])
}

func TestParseContent_InvalidJSON(t *testing.T) {
	data := []byte(`{invalid`)
	_, err := ParseContent(data, formatparse.FormatJSON, "test.json")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigFormat))
}

func TestParseContent_InvalidYAML(t *testing.T) {
	data := []byte(":\n  :\n    - ][")
	_, err := ParseContent(data, formatparse.FormatYAML, "test.yaml")
	assert.Error(t, err)
}

// ===========================================================================
// HTTPLoader with malformed URL to trigger NewRequestWithContext error
// ===========================================================================

func TestHTTPLoader_MalformedURL(t *testing.T) {
	// A URL with control characters should trigger NewRequestWithContext error.
	l := NewHTTP("http://example.com/\x00config.json")
	_, err := l.Load(context.Background())
	assert.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}
