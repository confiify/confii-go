// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws

package cloud

import (
	"context"
	"fmt"

	confii "github.com/confiify/confii-go"
)

func init() {
	confii.RegisterSelfConfigSourceProvider("s3", newSelfConfigS3)
	confii.RegisterSelfConfigSourceProvider("ssm", newSelfConfigSSM)
}

func newSelfConfigS3(_ context.Context, cfg map[string]any) (confii.Loader, error) {
	address := sourceString(cfg, "url", "uri")
	if address == "" {
		return nil, fmt.Errorf("s3 source requires url")
	}
	opts := make([]S3Option, 0, 4)
	if region := sourceString(cfg, "region"); region != "" {
		opts = append(opts, WithS3Region(region))
	}
	accessKey, secretKey, err := sourceCredentials(cfg)
	if err != nil {
		return nil, err
	}
	if accessKey != "" {
		opts = append(opts, WithS3Credentials(accessKey, secretKey))
	}
	if endpoint := sourceString(cfg, "endpoint", "endpoint_url"); endpoint != "" {
		opts = append(opts, WithS3Endpoint(endpoint))
	}
	pathStyle, err := sourceBool(cfg, "path_style", false)
	if err != nil {
		return nil, err
	}
	opts = append(opts, WithS3PathStyle(pathStyle))
	return NewS3(address, opts...)
}

func newSelfConfigSSM(_ context.Context, cfg map[string]any) (confii.Loader, error) {
	path := sourceString(cfg, "path", "prefix")
	if path == "" {
		return nil, fmt.Errorf("ssm source requires path")
	}
	opts := make([]SSMOption, 0, 4)
	if region := sourceString(cfg, "region"); region != "" {
		opts = append(opts, WithSSMRegion(region))
	}
	decrypt, err := sourceBool(cfg, "decrypt", true)
	if err != nil {
		return nil, err
	}
	opts = append(opts, WithSSMDecrypt(decrypt))
	accessKey, secretKey, err := sourceCredentials(cfg)
	if err != nil {
		return nil, err
	}
	if accessKey != "" {
		opts = append(opts, WithSSMCredentials(accessKey, secretKey))
	}
	if endpoint := sourceString(cfg, "endpoint", "endpoint_url"); endpoint != "" {
		opts = append(opts, WithSSMEndpoint(endpoint))
	}
	return NewSSM(path, opts...), nil
}
