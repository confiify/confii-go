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
	confii.RegisterSelfConfigSecretProvider("aws", func(cfg map[string]any) (confii.SelfConfigSecretStore, error) {
		var opts []AWSSecretsManagerOption
		if region := selfString(cfg, "region"); region != "" {
			opts = append(opts, WithAWSRegion(region))
		}
		accessKey := selfString(cfg, "access_key", "access_key_id")
		secretKey := selfString(cfg, "secret_key", "secret_access_key")
		if (accessKey == "") != (secretKey == "") {
			return nil, fmt.Errorf("aws provider requires both access_key and secret_key when either is set")
		}
		if accessKey != "" {
			opts = append(opts, WithAWSCredentials(accessKey, secretKey, selfString(cfg, "session_token")))
		}
		if endpoint := selfString(cfg, "endpoint", "endpoint_url"); endpoint != "" {
			opts = append(opts, WithAWSEndpoint(endpoint))
		}
		store, err := NewAWSSecretsManager(context.Background(), opts...)
		if err != nil {
			return nil, err
		}
		return selfConfigStoreAdapter{get: func(ctx context.Context, key string, secretOpts ...confii.SecretOption) (any, error) {
			return store.GetSecret(ctx, key, secretOpts...)
		}}, nil
	})
}
