// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build ibm

package cloud

import (
	"context"
	"fmt"

	confii "github.com/confiify/confii-go/v2"
)

func init() {
	confii.RegisterSelfConfigSourceProvider("ibm_cos", newSelfConfigIBMCOS)
	confii.RegisterSelfConfigSourceProvider("ibm-cos", newSelfConfigIBMCOS)
}

func newSelfConfigIBMCOS(_ context.Context, cfg map[string]any) (confii.Loader, error) {
	bucket := sourceString(cfg, "bucket")
	object := sourceString(cfg, "object", "key", "path")
	if bucket == "" || object == "" {
		return nil, fmt.Errorf("ibm_cos source requires bucket and object")
	}
	opts := make([]IBMCOSOption, 0, 2)
	if endpoint := sourceString(cfg, "endpoint", "endpoint_url"); endpoint != "" {
		opts = append(opts, WithIBMEndpoint(endpoint))
	}
	if region := sourceString(cfg, "region"); region != "" {
		opts = append(opts, WithIBMRegion(region))
	}
	return NewIBMCOS(bucket, object, opts...), nil
}
