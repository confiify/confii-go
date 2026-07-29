// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

package cloud

import (
	"context"
	"testing"

	confii "github.com/confiify/confii-go"
)

func TestAWSSelfConfigSourceRegistration(t *testing.T) {
	s3Factory, ok := confii.LookupSelfConfigSourceProvider("s3")
	if !ok {
		t.Fatal("s3 provider not registered")
	}
	loader, err := s3Factory(context.Background(), map[string]any{
		"url": "s3://bucket/config.yaml", "region": "eu-west-1",
		"access_key": "key", "secret_key": "secret",
		"endpoint": "http://127.0.0.1:4566", "path_style": true,
	})
	if err != nil {
		t.Fatalf("build s3 source: %v", err)
	}
	s3Loader := loader.(*S3Loader)
	if s3Loader.region != "eu-west-1" || s3Loader.accessKey != "key" ||
		s3Loader.endpoint != "http://127.0.0.1:4566" || !s3Loader.pathStyle {
		t.Fatalf("s3 options not applied: %#v", s3Loader)
	}
	if _, err := s3Factory(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected missing URL error")
	}
	if _, err := s3Factory(context.Background(), map[string]any{"url": "s3://bucket/key", "access_key": "incomplete"}); err == nil {
		t.Fatal("expected incomplete S3 credentials error")
	}
	if _, err := s3Factory(context.Background(), map[string]any{"url": "s3://bucket/key", "path_style": "bad"}); err == nil {
		t.Fatal("expected invalid S3 path_style error")
	}

	ssmFactory, ok := confii.LookupSelfConfigSourceProvider("ssm")
	if !ok {
		t.Fatal("ssm provider not registered")
	}
	loader, err = ssmFactory(context.Background(), map[string]any{
		"path": "/app/", "decrypt": false, "region": "us-east-2",
		"endpoint_url": "http://127.0.0.1:4566",
	})
	if err != nil {
		t.Fatalf("build ssm source: %v", err)
	}
	ssmLoader := loader.(*SSMLoader)
	if ssmLoader.decrypt || ssmLoader.region != "us-east-2" || ssmLoader.endpoint != "http://127.0.0.1:4566" {
		t.Fatalf("ssm options not applied: %#v", ssmLoader)
	}
	if _, err := ssmFactory(context.Background(), map[string]any{"path": "/app", "decrypt": "bad"}); err == nil {
		t.Fatal("expected invalid decrypt error")
	}
	if _, err := ssmFactory(context.Background(), map[string]any{}); err == nil {
		t.Fatal("expected missing SSM path error")
	}
	if _, err := ssmFactory(context.Background(), map[string]any{"path": "/app", "access_key": "incomplete"}); err == nil {
		t.Fatal("expected incomplete SSM credentials error")
	}
	loader, err = ssmFactory(context.Background(), map[string]any{
		"path": "/secure", "access_key": "key", "secret_key": "secret",
	})
	if err != nil || loader.(*SSMLoader).accessKey != "key" {
		t.Fatalf("expected explicit SSM credentials, loader=%#v err=%v", loader, err)
	}
}
