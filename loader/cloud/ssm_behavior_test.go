// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/stretchr/testify/require"
)

type ssmFixture struct {
	server     *httptest.Server
	lastTarget string
	lastBody   []byte
	respond    func(target string, body []byte, w http.ResponseWriter)
}

func newSSMFixture(t *testing.T, respond func(target string, body []byte, w http.ResponseWriter)) *ssmFixture {
	t.Helper()
	f := &ssmFixture{respond: respond}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		f.lastTarget = r.Header.Get("X-Amz-Target")
		f.lastBody = body
		f.respond(f.lastTarget, body, w)
	}))
	t.Cleanup(f.server.Close)

	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	t.Setenv("AWS_ENDPOINT_URL_SSM", f.server.URL)
	t.Setenv("AWS_ENDPOINT_URL", f.server.URL)
	return f
}

func TestSSMLoader_Load_HappyPath_ReturnsParsedKeys(t *testing.T) {
	f := newSSMFixture(t, func(target string, _ []byte, w http.ResponseWriter) {
		if !strings.Contains(target, "GetParametersByPath") {
			http.Error(w, "unexpected target: "+target, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Parameters": []map[string]any{
				{"Name": "/myapp/db/host", "Value": "db.example.com", "Type": "String"},
				{"Name": "/myapp/db/port", "Value": "5432", "Type": "String"},
			},
		})
	})

	l := NewSSM("/myapp/",
		WithSSMRegion("us-east-1"),
		WithSSMCredentials("AKIA-test", "secret-test"),
		WithSSMEndpoint(f.server.URL),
	)
	got, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	db, ok := got["db"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map under 'db', got %T (%#v)", got["db"], got)
	}
	if db["host"] != "db.example.com" {
		t.Errorf("db.host: got %#v, want %q", db["host"], "db.example.com")
	}

	if portStr := jsonStringify(db["port"]); portStr != "5432" {
		t.Errorf("db.port: got %s, want 5432", portStr)
	}

	if !strings.Contains(f.lastTarget, "GetParametersByPath") {
		t.Errorf("captured X-Amz-Target: got %q, want contains GetParametersByPath", f.lastTarget)
	}

	if !strings.Contains(string(f.lastBody), "/myapp/") {
		t.Errorf("captured body: got %q, want containing %q", string(f.lastBody), "/myapp/")
	}
}

func TestSSMLoader_LoadCanceledContextWrapsConfigError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := NewSSM("/canceled/", WithSSMRegion("us-east-1"))
	_, err := l.Load(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, confii.ErrConfigLoad)
}

func TestSSMLoader_Load_ServerError_ReturnsConfigLoadError(t *testing.T) {
	_ = newSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"__type":"InternalServerError","message":"boom"}`))
	})

	l := NewSSM("/myapp/",
		WithSSMRegion("us-east-1"),
		WithSSMCredentials("AKIA-test", "secret-test"),
	)
	_, err := l.Load(context.Background())
	if err == nil {
		t.Fatal("expected error from Load on 500, got nil")
	}
	if !errors.Is(err, confii.ErrConfigLoad) {
		t.Errorf("Load error not wrapping ErrConfigLoad: %v", err)
	}
	var ce *confii.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("Load error not a *confii.ConfigError: %T (%v)", err, err)
	}
	if ce.Source != "ssm:/myapp/" {
		t.Errorf("ConfigError.Source: got %q, want ssm:/myapp/", ce.Source)
	}
}

func TestSSMLoader_Load_EmptyResponse_ReturnsNil(t *testing.T) {
	_ = newSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Parameters": []map[string]any{},
		})
	})

	l := NewSSM("/empty/",
		WithSSMRegion("us-east-1"),
		WithSSMCredentials("AKIA-test", "secret-test"),
	)
	got, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Errorf("Load: got %#v, want nil for empty response", got)
	}
}

func TestSSMLoader_Load_MissingCredentials_StillReachesEndpoint(t *testing.T) {
	called := false
	f := newSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		called = true
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"Parameters": []map[string]any{}})
	})

	t.Setenv("AWS_ACCESS_KEY_ID", "anon")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "anon")

	l := NewSSM("/anon/", WithSSMRegion("us-east-1"))
	if _, err := l.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !called {
		t.Fatal("expected fixture server to be hit, but it was not")
	}
	_ = f
}

func jsonStringify(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
