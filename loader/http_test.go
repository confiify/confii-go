// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package loader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	result, err := ParseContent([]byte("database:\n  host: yaml-without-metadata\n"), FormatUnknown, "https://example.test/config")
	require.NoError(t, err)
	database, ok := result["database"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "yaml-without-metadata", database["host"])
}

func TestParseContent_SelectedYAMLRejectsJSON(t *testing.T) {
	_, err := ParseContent([]byte(`{"server":{"port":8080}}`), FormatYAML, "https://example.test/config.yaml")
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigFormat)
	assert.Contains(t, err.Error(), "JSON document")

	result, err := ParseContent([]byte(`{"server":{"port":8080}}`), FormatUnknown, "https://example.test/config")
	require.NoError(t, err, "unknown format retains explicit auto-detection")
	assert.NotNil(t, result["server"])
}

func TestParseContent_UnknownReportsBothParserFailures(t *testing.T) {
	_, err := ParseContent([]byte("[unterminated"), FormatUnknown, "https://example.test/config")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON parse")
	assert.Contains(t, err.Error(), "YAML parse")
}

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

	assert.EqualValues(t, 5432, db["port"])
}

func TestHTTPLoader_ParseTOMLByExtension(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("name = \"ext-host\"\nport = 42\n"))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL + "/config.toml")

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

func TestHTTPLoader_BasicAuthSourceAndBodyReadError(t *testing.T) {
	srv := newHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "user" || password != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"short":true}`))
	}))
	defer srv.Close()

	l := NewHTTP(srv.URL, WithBasicAuth("user", "password"))
	assert.Equal(t, srv.URL, l.Source())
	_, err := l.Load(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
}

func TestHTTPLoader_RequestAndTransportErrors(t *testing.T) {
	_, err := NewHTTP("://bad-url").Load(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewHTTP("http://127.0.0.1:1", WithTimeout(1)).Load(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, confii.ErrConfigLoad)
}

func TestParseContent_ExplicitFormatsAndUnsupported(t *testing.T) {
	tests := []struct {
		format Format
		data   string
	}{
		{FormatJSON, `{"ok":true}`},
		{FormatYAML, "ok: true\n"},
		{FormatTOML, "ok = true\n"},
	}
	for _, tc := range tests {
		t.Run(string(tc.format), func(t *testing.T) {
			got, err := ParseContent([]byte(tc.data), tc.format, "source")
			require.NoError(t, err)
			assert.Equal(t, true, got["ok"])
		})
	}

	_, err := ParseContent([]byte("irrelevant"), Format("xml"), "source")
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("unsupported format %q", "xml"))
}
