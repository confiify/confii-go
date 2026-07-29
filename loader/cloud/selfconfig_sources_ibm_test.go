// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build ibm

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestIBMSelfConfigSourceRegistration(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSourceProvider("ibm_cos")
	if !ok {
		t.Fatal("ibm_cos provider not registered")
	}
	loader, err := factory(context.Background(), map[string]any{
		"bucket": "config-bucket", "object": "app.yaml", "region": "eu-de", "endpoint": "https://cos.example.invalid",
	})
	if err != nil {
		t.Fatalf("build IBM COS source: %v", err)
	}
	ibmLoader := loader.(*IBMCOSLoader)
	if ibmLoader.region != "eu-de" || ibmLoader.endpointURL != "https://cos.example.invalid" {
		t.Fatalf("IBM options not applied: %#v", ibmLoader)
	}
	if _, err := factory(context.Background(), map[string]any{"bucket": "only"}); err == nil {
		t.Fatal("expected missing object error")
	}
}
