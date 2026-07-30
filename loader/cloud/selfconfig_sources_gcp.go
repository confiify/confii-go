// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build gcp

package cloud

import (
	"context"
	"fmt"

	confii "github.com/confiify/confii-go/v2"
)

func init() {
	confii.RegisterSelfConfigSourceProvider("gcs", newSelfConfigGCS)
}

func newSelfConfigGCS(_ context.Context, cfg map[string]any) (confii.Loader, error) {
	bucket := sourceString(cfg, "bucket")
	object := sourceString(cfg, "object", "blob", "path")
	if bucket == "" || object == "" {
		return nil, fmt.Errorf("gcs source requires bucket and object")
	}
	opts := make([]GCSOption, 0, 2)
	if project := sourceString(cfg, "project", "project_id"); project != "" {
		opts = append(opts, WithGCSProject(project))
	}
	if credentials := sourceString(cfg, "credentials_file", "credentials_path"); credentials != "" {
		opts = append(opts, WithGCSCredentials(credentials))
	}
	return NewGCS(bucket, object, opts...), nil
}
