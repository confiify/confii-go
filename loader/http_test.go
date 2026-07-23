package loader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	confii "github.com/confiify/confii-go"
	"github.com/confiify/confii-go/internal/formatparse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// G35: HTTP tests use httptest.NewServer for hermetic-environment safety.
// httptest.NewServer binds to 127.0.0.1:0 and reports the actual URL via
// srv.URL, so the OS picks a free ephemeral port and the test never
// hard-codes a port. The newHTTPTestServer helper wraps httptest.NewServer
// to convert a loopback-bind panic (sandboxed/hermetic CI runners that
// reject any 127.0.0.1 bind) into a t.Skip with a clear reason rather
// than failing the suite.

func newHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("loopback bind unavailable (hermetic environment): %v", r)
		}
	}()
	return httptest.NewServer(handler)
}

func TestHTTPLoader_Load_JSON(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"database":{"host":"remote-db"},"port":5432}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "remote-db", db["host"])
}

func TestHTTPLoader_Load_YAML(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write([]byte("database:\n  host: yaml-host\n"))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "yaml-host", db["host"])
}

func TestHTTPLoader_Load_Error(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL)
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, confii.ErrConfigLoad))
}

func TestParseContent_UnknownFallsBackFromJSONToYAML(t *testing.T) {
	result, err := ParseContent([]byte("database:\n  host: yaml-without-metadata\n"), formatparse.FormatUnknown, "https://example.test/config")
	require.NoError(t, err)
	database, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "yaml-without-metadata", database["host"])
}

func TestParseContent_UnknownReportsBothParserFailures(t *testing.T) {
	_, err := ParseContent([]byte("[unterminated"), formatparse.FormatUnknown, "https://example.test/config")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON parse")
	assert.Contains(t, err.Error(), "YAML parse")
}

// TestHTTPLoader_ParseTOMLContent covers G19: the HTTP loader detects
// `application/toml` Content-Type and parses the body via the shared
// TOML parser used by the file-based [TOMLLoader].
func TestHTTPLoader_ParseTOMLContent(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/toml")
		_, _ = w.Write([]byte("[database]\nhost = \"toml-host\"\nport = 5432\n"))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL)
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	db, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "toml-host", db["host"])
	// BurntSushi/toml decodes integers as int64.
	assert.EqualValues(t, 5432, db["port"])
}

// TestHTTPLoader_ParseTOMLByExtension covers G19: when the server omits
// or sets an unhelpful Content-Type, the loader falls back to the URL
// extension, and a `.toml` URL must still route to the TOML parser.
func TestHTTPLoader_ParseTOMLByExtension(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately unhelpful Content-Type to force extension-based
		// detection in (*HTTPLoader).Load.
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("name = \"ext-host\"\nport = 42\n"))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL + "/config.toml")
	// httptest.NewServer routes any path to the same handler, so the
	// trailing `.toml` only influences format detection here.
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ext-host", result["name"])
	assert.EqualValues(t, 42, result["port"])
}

func TestHTTPLoader_WithHeaders(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL, WithHeaders(map[string]string{"X-Custom": "test"}))
	result, err := l.Load(context.Background())
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])
}
