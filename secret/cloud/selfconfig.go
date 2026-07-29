// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

//go:build aws || azure || gcp || vault

package cloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	confii "github.com/confiify/confii-go"
)

type selfConfigStoreAdapter struct {
	get func(context.Context, string, ...confii.SecretOption) (any, error)
}

func (s selfConfigStoreAdapter) GetSecret(ctx context.Context, key string) (any, error) {
	return s.get(ctx, key)
}

func (s selfConfigStoreAdapter) GetSecretRequest(ctx context.Context, request confii.SelfConfigSecretRequest) (any, error) {
	var opts []confii.SecretOption
	if request.Version != "" {
		opts = append(opts, confii.WithVersion(request.Version))
	}
	return s.get(ctx, request.Key, opts...)
}

func selfString(cfg map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := cfg[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func selfBool(cfg map[string]any, key string, fallback bool) (bool, error) {
	v, ok := cfg[key]
	if !ok {
		return fallback, nil
	}
	switch value := v.(type) {
	case bool:
		return value, nil
	case string:
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed, nil
		}
	}
	return false, fmt.Errorf("%s must be a boolean", key)
}

func selfInt(cfg map[string]any, key string, fallback int) (int, error) {
	v, ok := cfg[key]
	if !ok {
		return fallback, nil
	}
	switch value := v.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	case string:
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("%s must be an integer", key)
}
