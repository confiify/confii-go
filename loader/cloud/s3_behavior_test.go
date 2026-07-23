//go:build aws

// Behavior tests for the AWS S3 configuration loader.
//
// Provider: AWS S3 object storage (loader/cloud/s3.go).
//
// Fixture mechanism: protocol-level / constructor exercises only. The
// production S3 loader instantiates an SDK client via
// `s3.NewFromConfig(cfg)` with no `UsePathStyle` toggle and no exported
// endpoint override option (see the matching `secret/cloud/aws.go`
// `WithAWSEndpoint` for contrast). The AWS SDK v2 default uses
// virtual-hosted addressing (`https://<bucket>.<endpoint>`), which cannot
// be redirected to a `httptest.NewServer` listening on a loopback port —
// the `<bucket>.127.0.0.1:<port>` host does not resolve. Until a
// `WithS3Endpoint` / `WithS3PathStyle` option is added (see G26
// follow-up), full Load() behavior cannot be asserted from a hermetic in-
// process fixture, only the URL-parsing and credential-plumbing paths.
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
	"errors"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go"
)

// TestS3Loader_NewS3_HappyPath_ParsesURL exercises the constructor's
// happy path: a well-formed s3:// URL is split into bucket and key.
func TestS3Loader_NewS3_HappyPath_ParsesURL(t *testing.T) {
	l, err := NewS3("s3://my-bucket/path/to/config.yaml")
	if err != nil {
		t.Fatalf("NewS3 returned error for valid URL: %v", err)
	}
	if l.bucket != "my-bucket" {
		t.Errorf("bucket: got %q, want %q", l.bucket, "my-bucket")
	}
	if l.key != "path/to/config.yaml" {
		t.Errorf("key: got %q, want %q", l.key, "path/to/config.yaml")
	}
	if got, want := l.Source(), "s3://my-bucket/path/to/config.yaml"; got != want {
		t.Errorf("Source: got %q, want %q", got, want)
	}
}

// TestS3Loader_NewS3_RejectsNonS3Scheme verifies that the constructor
// surfaces a typed error when the URL scheme is not "s3".
func TestS3Loader_NewS3_RejectsNonS3Scheme(t *testing.T) {
	_, err := NewS3("https://my-bucket/path/to/config.yaml")
	if err == nil {
		t.Fatal("expected error for non-s3 scheme, got nil")
	}
	if !strings.Contains(err.Error(), "expected s3:// scheme") {
		t.Errorf("error message: got %q, want containing %q", err.Error(), "expected s3:// scheme")
	}
}

// TestS3Loader_NewS3_RejectsMalformedURL verifies that parse errors
// from the URL parser are wrapped and returned.
func TestS3Loader_NewS3_RejectsMalformedURL(t *testing.T) {
	// A URL with an invalid control character causes url.Parse to fail.
	_, err := NewS3("s3://bucket/\x7f")
	if err == nil {
		t.Fatal("expected parse error for malformed URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid S3 URL") {
		t.Errorf("error message: got %q, want containing %q", err.Error(), "invalid S3 URL")
	}
}

// TestS3Loader_WithS3Credentials_PopulatesFields verifies that the
// option pipeline records explicit credentials so they would be passed
// to the AWS config loader at Load() time.
func TestS3Loader_WithS3Credentials_PopulatesFields(t *testing.T) {
	l, err := NewS3(
		"s3://b/k.json",
		WithS3Region("eu-west-1"),
		WithS3Credentials("AKIA-test", "secret-test"),
	)
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	if l.region != "eu-west-1" {
		t.Errorf("region: got %q, want %q", l.region, "eu-west-1")
	}
	if l.accessKey != "AKIA-test" || l.secretKey != "secret-test" {
		t.Errorf("credentials: got (%q,%q), want (AKIA-test,secret-test)",
			l.accessKey, l.secretKey)
	}
}

// TestS3Loader_Load_FailsWithoutResolvableEndpoint exercises the error
// path for Load(): with a bogus region and bogus bucket the SDK should
// fail to issue a request and the loader should wrap the error in a
// ConfigError. This is a smoke test of the error-wrapping contract; it
// does NOT validate the success path because the production loader does
// not expose a path-style endpoint override (see file header for the
// G26-territory rationale).
func TestS3Loader_Load_FailsWithoutResolvableEndpoint(t *testing.T) {
	// Use an obviously-unreachable endpoint via env so the SDK does not
	// reach out to real AWS during this hermetic test.
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ENDPOINT_URL_S3", "http://127.0.0.1:1") // closed port

	l, err := NewS3(
		"s3://bucket-does-not-exist-confii-test/k.json",
		WithS3Region("us-east-1"),
		WithS3Credentials("AKIA-test", "secret-test"),
	)
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel to force a fast failure regardless of SDK timeouts.

	_, loadErr := l.Load(ctx)
	if loadErr == nil {
		t.Fatal("expected error from Load against unreachable endpoint, got nil")
	}
	// The wrapper must produce a ConfigError tagged with ErrConfigLoad.
	var ce *confii.ConfigError
	if !errors.As(loadErr, &ce) {
		t.Fatalf("Load error not a *confii.ConfigError: %T (%v)", loadErr, loadErr)
	}
	if ce.Op != "Load" {
		t.Errorf("ConfigError.Op: got %q, want %q", ce.Op, "Load")
	}
	if ce.Source != "s3://bucket-does-not-exist-confii-test/k.json" {
		t.Errorf("ConfigError.Source: got %q, want s3 URL", ce.Source)
	}
}
