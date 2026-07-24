// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

// Behavior tests for the AWS Secrets Manager secret store.
//
// Provider: AWS Secrets Manager (secret/cloud/aws.go).
//
// Fixture mechanism: protocol-level fixture via `httptest.NewServer`
// that mimics the Secrets Manager JSON-RPC endpoint. The store is
// constructed with `WithAWSEndpoint(server.URL)`, which the production
// code passes through to the SDK as `secretsmanager.Options.BaseEndpoint`
// — so the fixture intercepts every call without any global env-var
// hackery.
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

// awsSMFixture wires a httptest server up as a fake Secrets Manager
// endpoint. The fixture also captures the X-Amz-Target header (which
// identifies the SecretsManager operation) and the request body so
// individual tests can assert what the store actually sent.
type awsSMFixture struct {
	server     *httptest.Server
	lastTarget string
	lastBody   []byte
	respond    func(target string, body []byte, w http.ResponseWriter)
}

func newAWSSMFixture(t *testing.T, respond func(target string, body []byte, w http.ResponseWriter)) *awsSMFixture {
	t.Helper()
	f := &awsSMFixture{respond: respond}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		f.lastTarget = r.Header.Get("X-Amz-Target")
		f.lastBody = body
		f.respond(f.lastTarget, body, w)
	}))
	t.Cleanup(f.server.Close)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	return f
}

func newStoreAgainstFixture(t *testing.T, f *awsSMFixture) *AWSSecretsManager {
	t.Helper()
	store, err := NewAWSSecretsManager(
		context.Background(),
		WithAWSRegion("us-east-1"),
		WithAWSCredentials("AKIA-test", "secret-test", ""),
		WithAWSEndpoint(f.server.URL),
	)
	if err != nil {
		t.Fatalf("NewAWSSecretsManager: %v", err)
	}
	return store
}

// TestAWSSM_GetSecret_HappyPath_StringValue covers the simplest
// success path: a plain-string secret is returned as a Go string.
func TestAWSSM_GetSecret_HappyPath_StringValue(t *testing.T) {
	f := newAWSSMFixture(t, func(target string, _ []byte, w http.ResponseWriter) {
		if !strings.Contains(target, "GetSecretValue") {
			http.Error(w, "unexpected target: "+target, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ARN":          "arn:aws:secretsmanager:us-east-1:123:secret:my-secret",
			"Name":         "my-secret",
			"SecretString": "p@ssw0rd",
		})
	})
	store := newStoreAgainstFixture(t, f)

	got, err := store.GetSecret(context.Background(), "my-secret")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "p@ssw0rd" {
		t.Errorf("GetSecret: got %#v, want %q", got, "p@ssw0rd")
	}
}

// TestAWSSM_GetSecret_HappyPath_JSONExtraction covers the documented
// "secret_name:json_key" syntax: the store should parse the SecretString
// as JSON and return only the requested field.
func TestAWSSM_GetSecret_HappyPath_JSONExtraction(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"SecretString": `{"username":"alice","password":"hunter2"}`,
		})
	})
	store := newStoreAgainstFixture(t, f)

	got, err := store.GetSecret(context.Background(), "db-creds:password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("GetSecret: got %#v, want %q", got, "hunter2")
	}
}

// TestAWSSM_GetSecret_JSONKeyNotFound exercises the validation-error
// branch of the JSON extraction path: when the JSON parses but the
// requested key is absent, the store must surface ErrSecretValidation
// rather than a stale value or nil.
func TestAWSSM_GetSecret_JSONKeyNotFound(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"SecretString": `{"username":"alice"}`,
		})
	})
	store := newStoreAgainstFixture(t, f)

	_, err := store.GetSecret(context.Background(), "db-creds:password")
	if err == nil {
		t.Fatal("expected error for missing JSON key, got nil")
	}
	if !errors.Is(err, confii.ErrSecretValidation) {
		t.Errorf("error: got %v, want wrapping ErrSecretValidation", err)
	}
}

// TestAWSSM_GetSecret_NotFound_ReturnsErrSecretNotFound exercises the
// AWS-specific error mapping: a ResourceNotFoundException response from
// SecretsManager must surface as confii.ErrSecretNotFound, not as a
// generic access error.
func TestAWSSM_GetSecret_NotFound_ReturnsErrSecretNotFound(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		// SecretsManager returns 400 with a typed __type body for
		// resource-not-found.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"Secrets Manager can't find the specified secret."}`))
	})
	store := newStoreAgainstFixture(t, f)

	_, err := store.GetSecret(context.Background(), "missing-secret")
	if err == nil {
		t.Fatal("expected ErrSecretNotFound, got nil")
	}
	if !errors.Is(err, confii.ErrSecretNotFound) {
		t.Errorf("error: got %v, want wrapping ErrSecretNotFound", err)
	}
}

// TestAWSSM_GetSecret_ServerError_ReturnsErrSecretAccess exercises the
// generic-access-error branch: a 500 from SecretsManager must surface
// as confii.ErrSecretAccess, distinguishable from ErrSecretNotFound.
func TestAWSSM_GetSecret_ServerError_ReturnsErrSecretAccess(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"__type":"InternalFailure","message":"boom"}`))
	})
	store := newStoreAgainstFixture(t, f)

	_, err := store.GetSecret(context.Background(), "any-secret")
	if err == nil {
		t.Fatal("expected ErrSecretAccess, got nil")
	}
	if errors.Is(err, confii.ErrSecretNotFound) {
		t.Errorf("error: got ErrSecretNotFound, want ErrSecretAccess for 500 response: %v", err)
	}
	if !errors.Is(err, confii.ErrSecretAccess) {
		t.Errorf("error: got %v, want wrapping ErrSecretAccess", err)
	}
}

// TestAWSSM_GetSecret_VersionStage exercises the version-routing logic:
// when the caller passes a known stage name (AWSCURRENT/AWSPREVIOUS/
// AWSPENDING), the store should populate VersionStage in the request
// payload, not VersionId. We assert via the captured request body.
func TestAWSSM_GetSecret_VersionStage(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"SecretString": "ok"})
	})
	store := newStoreAgainstFixture(t, f)

	if _, err := store.GetSecret(
		context.Background(),
		"my-secret",
		confii.WithVersion("AWSPREVIOUS"),
	); err != nil {
		t.Fatalf("GetSecret: %v", err)
	}

	if !strings.Contains(string(f.lastBody), "AWSPREVIOUS") {
		t.Errorf("request body: got %q, want containing AWSPREVIOUS", string(f.lastBody))
	}
	if !strings.Contains(string(f.lastBody), "VersionStage") {
		t.Errorf("request body: got %q, want containing VersionStage", string(f.lastBody))
	}
	if strings.Contains(string(f.lastBody), "VersionId") {
		t.Errorf("request body: got %q, want NOT containing VersionId for stage", string(f.lastBody))
	}
}

// TestAWSSM_GetSecret_VersionID exercises the inverse routing: a
// non-stage version string must be sent as VersionId.
func TestAWSSM_GetSecret_VersionID(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"SecretString": "ok"})
	})
	store := newStoreAgainstFixture(t, f)

	if _, err := store.GetSecret(
		context.Background(),
		"my-secret",
		confii.WithVersion("ad9b8e2f-1234-5678-90ab-cdef01234567"),
	); err != nil {
		t.Fatalf("GetSecret: %v", err)
	}

	if !strings.Contains(string(f.lastBody), "VersionId") {
		t.Errorf("request body: got %q, want containing VersionId", string(f.lastBody))
	}
	if strings.Contains(string(f.lastBody), "VersionStage") {
		t.Errorf("request body: got %q, want NOT containing VersionStage for explicit ID", string(f.lastBody))
	}
}

// TestAWSSM_NewAWSSecretsManager_AuthPlumbing verifies the constructor
// happy path threads explicit credentials, region, and endpoint through
// without error. This is the auth-path smoke test for the store: a
// missing/invalid SDK config would surface here.
func TestAWSSM_NewAWSSecretsManager_AuthPlumbing(t *testing.T) {
	store, err := NewAWSSecretsManager(
		context.Background(),
		WithAWSRegion("eu-west-1"),
		WithAWSCredentials("AKIA-test", "secret-test", "session-token"),
		WithAWSEndpoint("http://example.invalid"),
	)
	if err != nil {
		t.Fatalf("NewAWSSecretsManager: %v", err)
	}
	if store == nil || store.client == nil {
		t.Fatal("expected non-nil store with non-nil client")
	}
}
