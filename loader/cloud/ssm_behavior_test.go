//go:build aws

// Behavior tests for the AWS Systems Manager Parameter Store (SSM)
// configuration loader.
//
// Provider: AWS SSM Parameter Store (loader/cloud/ssm.go).
//
// Fixture mechanism: protocol-level fixture via `httptest.NewServer`
// returning canned `GetParametersByPath` JSON responses. The AWS SDK v2
// uses JSON-RPC over HTTPS for SSM (X-Amz-Target header carrying the
// operation name), so a plain `httptest` server suffices to simulate the
// service. The test redirects the SDK to the fixture server with the
// `AWS_ENDPOINT_URL_SSM` environment variable, which the SDK respects in
// `config.LoadDefaultConfig` since v1.27.
//
// Build-tag gating: this file is gated by `//go:build aws`; the cloud module
// owns the required AWS SDK versions. Upstream CI exercises it through the
// same temporary consumer shape users install.
//
// Running locally as a consumer:
//
//	go test -tags aws -count=1 ./...

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

	confii "github.com/confiify/confii-go"
)

// ssmFixture wires a httptest server up as a fake SSM endpoint and
// redirects the AWS SDK at it via env vars. The returned cleanup is
// registered with t.Cleanup and the env is reset by t.Setenv.
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

	// Disable IMDS lookups so creds resolution stays hermetic.
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	// Force the SDK at our fixture server.
	t.Setenv("AWS_ENDPOINT_URL_SSM", f.server.URL)
	t.Setenv("AWS_ENDPOINT_URL", f.server.URL)
	return f
}

// TestSSMLoader_Load_HappyPath_ReturnsParsedKeys verifies the success
// path: the loader issues a GetParametersByPath, the fixture responds
// with two parameters, and the loader nests them by "/" into the
// returned map.
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

	l := NewSSM("/myapp/", WithSSMRegion("us-east-1"), WithSSMCredentials("AKIA-test", "secret-test"))
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
	// SSMLoader's Load calls typecoerce.ParseScalar, which converts
	// numeric-looking strings to numbers; assert via stringification so
	// the test does not depend on the exact numeric type.
	if portStr := jsonStringify(db["port"]); portStr != "5432" {
		t.Errorf("db.port: got %s, want 5432", portStr)
	}

	if !strings.Contains(f.lastTarget, "GetParametersByPath") {
		t.Errorf("captured X-Amz-Target: got %q, want contains GetParametersByPath", f.lastTarget)
	}
	// Confirm the request body carried the configured prefix (with
	// trailing slash appended by NewSSM).
	if !strings.Contains(string(f.lastBody), "/myapp/") {
		t.Errorf("captured body: got %q, want containing %q", string(f.lastBody), "/myapp/")
	}
}

// TestSSMLoader_Load_ServerError_ReturnsConfigLoadError exercises the
// 500 error path: the loader must wrap the SDK error in a ConfigError
// tagged with ErrConfigLoad.
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

// TestSSMLoader_Load_EmptyResponse_ReturnsNil verifies the
// "no parameters" path: the loader returns (nil, nil) when the response
// is empty, matching the documented contract for "missing source =
// empty contribution."
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

// TestSSMLoader_Load_MissingCredentials_StillReachesEndpoint exercises
// the auth-plumbing path. With no credentials configured on the loader
// AND no ambient credentials, the SDK should still attempt the request
// against the configured endpoint (because the test fixture sits
// "inside" AWS), and we assert that the request was issued. This guards
// against regressions where the loader fails before contacting the
// service when credentials are absent. (Production AWS would reject
// such a request, but the fixture has no auth gate.)
func TestSSMLoader_Load_MissingCredentials_StillReachesEndpoint(t *testing.T) {
	called := false
	f := newSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		called = true
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"Parameters": []map[string]any{}})
	})
	// Provide *anonymous* creds via env so the SDK signer doesn't fail
	// on missing credentials before issuing the request. This is the
	// minimal credential surface needed to exercise the transport path.
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

// jsonStringify renders v with json.Marshal so numeric/string scalars
// can be compared without locking to a specific Go numeric type.
func jsonStringify(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
