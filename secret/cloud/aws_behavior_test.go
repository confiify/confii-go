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
)

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
	store, err := NewAWSSecretsManager(context.Background(),
		WithAWSRegion("us-east-1"),
		WithAWSCredentials("AKIA-test", "secret-test", ""),
		WithAWSEndpoint(f.server.URL),
	)
	if err != nil {
		t.Fatalf("NewAWSSecretsManager: %v", err)
	}
	return store
}

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

func TestAWSSM_GetSecret_HappyPath_JSONExtraction(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"SecretString": `{"username":"alice","password":"hunter2"}`,
		})
	})
	store := newStoreAgainstFixture(t, f)

	got, err := store.GetSecret(context.Background(), "db-creds", confii.WithField("password"))
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("GetSecret: got %#v, want %q", got, "hunter2")
	}
}

func TestAWSSM_GetSecret_JSONKeyNotFound(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"SecretString": `{"username":"alice"}`,
		})
	})
	store := newStoreAgainstFixture(t, f)

	_, err := store.GetSecret(context.Background(), "db-creds", confii.WithField("password"))
	if err == nil {
		t.Fatal("expected error for missing JSON key, got nil")
	}
	if !errors.Is(err, confii.ErrSecretValidation) {
		t.Errorf("error: got %v, want wrapping ErrSecretValidation", err)
	}
}

func TestAWSSM_GetSecret_NotFound_ReturnsErrSecretNotFound(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")

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

func TestAWSSM_GetSecret_VersionStage(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"SecretString": "ok"})
	})
	store := newStoreAgainstFixture(t, f)

	if _, err := store.GetSecret(context.Background(),
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

func TestAWSSM_GetSecret_VersionID(t *testing.T) {
	f := newAWSSMFixture(t, func(_ string, _ []byte, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"SecretString": "ok"})
	})
	store := newStoreAgainstFixture(t, f)

	if _, err := store.GetSecret(context.Background(),
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

func TestAWSSM_NewAWSSecretsManager_AuthPlumbing(t *testing.T) {
	store, err := NewAWSSecretsManager(context.Background(),
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
