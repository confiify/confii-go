//go:build aws || azure || gcp || vault

package cloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type selfConfigStoreAdapter struct {
	get func(context.Context, string) (any, error)
}

func (s selfConfigStoreAdapter) GetSecret(ctx context.Context, key string) (any, error) {
	return s.get(ctx, key)
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
