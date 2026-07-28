// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package cloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	confii "github.com/confiify/confii-go"
)

func init() {
	confii.RegisterSelfConfigSourceProvider("git", newSelfConfigGit)
}

func newSelfConfigGit(_ context.Context, cfg map[string]any) (confii.Loader, error) {
	repository := sourceString(cfg, "repository", "repo_url", "url")
	path := sourceString(cfg, "file", "file_path", "path")
	if repository == "" || path == "" {
		return nil, fmt.Errorf("git source requires repository and file_path")
	}
	opts := make([]GitOption, 0, 2)
	if branch := sourceString(cfg, "branch", "ref"); branch != "" {
		opts = append(opts, WithGitBranch(branch))
	}
	if token := sourceString(cfg, "token"); token != "" {
		opts = append(opts, WithGitToken(token))
	}
	return NewGit(repository, path, opts...), nil
}

func sourceString(cfg map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := cfg[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sourceBool(cfg map[string]any, key string, fallback bool) (bool, error) {
	value, ok := cfg[key]
	if !ok {
		return fallback, nil
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err == nil {
			return parsed, nil
		}
	}
	return false, fmt.Errorf("%s must be a boolean", key)
}

func sourceCredentials(cfg map[string]any) (string, string, error) {
	accessKey := sourceString(cfg, "access_key", "access_key_id")
	secretKey := sourceString(cfg, "secret_key", "secret_access_key")
	if (accessKey == "") != (secretKey == "") {
		return "", "", fmt.Errorf("access_key and secret_key must be configured together")
	}
	return accessKey, secretKey, nil
}
