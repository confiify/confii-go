// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build gcp

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestGCPSelfConfigSourceRegistration(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSourceProvider("gcs")
	if !ok {
		t.Fatal("gcs provider not registered")
	}
	loader, err := factory(context.Background(), map[string]any{
		"bucket": "config-bucket", "object": "app.yaml", "project_id": "project", "credentials_file": "/tmp/credentials.json",
	})
	if err != nil {
		t.Fatalf("build gcs source: %v", err)
	}
	gcsLoader := loader.(*GCSLoader)
	if gcsLoader.projectID != "project" || gcsLoader.credentialsPath != "/tmp/credentials.json" {
		t.Fatalf("gcs options not applied: %#v", gcsLoader)
	}
	if _, err := factory(context.Background(), map[string]any{"bucket": "only"}); err == nil {
		t.Fatal("expected missing object error")
	}
}
