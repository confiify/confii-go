// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/hook"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/confiify/confii-go/v2/secret"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructuredResolverWorksForEveryConsumerFormat(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte("shared:\n  port: 8080\n"), 0o600))

	tests := []struct {
		name    string
		file    string
		body    string
		newLoad func(string) confii.Loader
		key     string
		want    any
	}{
		{"yaml", "app.yaml", "port: ${yaml:shared.yaml#shared.port}\n", func(path string) confii.Loader { return loader.NewYAML(path) }, "port", 8080},
		{"json", "app.json", `{"port":"${yaml:shared.yaml#shared.port}"}`, func(path string) confii.Loader { return loader.NewJSON(path) }, "port", 8080},
		{"toml", "app.toml", `port = "${yaml:shared.yaml#shared.port}"`, func(path string) confii.Loader { return loader.NewTOML(path) }, "port", 8080},
		{"ini", "app.ini", "document = ${yaml:shared.yaml}\n", func(path string) confii.Loader { return loader.NewINI(path) }, "document", map[string]any{"shared": map[string]any{"port": 8080}}},
		{"dotenv", ".env", "port=${yaml:shared.yaml#shared.port}\n", func(path string) confii.Loader { return loader.NewEnvFile(path) }, "port", 8080},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.file)
			require.NoError(t, os.WriteFile(path, []byte(tt.body), 0o600))
			cfg, err := confii.New[any](
				confii.WithWorkingDir(dir),
				confii.WithLoaders(tt.newLoad(path)),
				confii.WithEnvExpander(false),
				confii.WithStructuredResolver(true),
				confii.WithTypeCasting(false),
			)
			require.NoError(t, err)
			got, err := cfg.Get(tt.key)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStructuredResolverSelfAndSecrets(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.json"), []byte(`{"token":"${secret:token}"}`), 0o600))
	resolver := secret.NewResolver(secret.NewDictStore(map[string]any{"token": "resolved-token"}))

	cfg, err := confii.New[any](
		confii.WithWorkingDir(dir),
		confii.WithLoaders(&hooksTestLoader{source: "stub", data: map[string]any{
			"defaults": map[string]any{"host": "localhost"},
			"host":     "${json:self#defaults.host}",
			"token":    "${json:shared.json#token}",
		}}),
		confii.WithEnvExpander(false),
		confii.WithStructuredResolver(true),
		confii.WithTypeCasting(false),
		confii.WithSecretResolver(resolver),
	)
	require.NoError(t, err)

	host, err := cfg.Get("host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host)
	token, err := cfg.Get("token")
	require.NoError(t, err)
	assert.Equal(t, "resolved-token", token)
}

func TestURLCommandAndCustomResolversAreOptIn(t *testing.T) {
	cfg, err := confii.New[any](
		confii.WithLoaders(&hooksTestLoader{source: "stub", data: map[string]any{
			"remote": "${url:https://example.test/config.txt}",
			"cmd":    "${cmd:printf from-cmd}",
			"custom": "${upper:custom-value}",
		}}),
		confii.WithEnvExpander(false),
		confii.WithURLResolver(true),
		confii.WithCommandResolver(true),
		confii.WithTypeCasting(false),
		confii.WithValueResolver("url", func(_ context.Context, req hook.ResolverRequest) (any, error) {
			return "custom-url:" + req.Target, nil
		}),
		confii.WithValueResolver("upper", func(_ context.Context, req hook.ResolverRequest) (any, error) {
			return strings.ToUpper(req.Target), nil
		}),
	)
	require.NoError(t, err)

	for key, want := range map[string]string{
		"remote": "custom-url:https://example.test/config.txt",
		"cmd":    "from-cmd",
		"custom": "CUSTOM-VALUE",
	} {
		got, err := cfg.Get(key)
		require.NoError(t, err)
		assert.Equal(t, want, got, fmt.Sprintf("key %s", key))
	}
}

func TestDisabledResolversLeavePlaceholdersUnchanged(t *testing.T) {
	cfg, err := confii.New[any](
		confii.WithLoaders(&hooksTestLoader{source: "stub", data: map[string]any{
			"value": "${cmd:printf nope}",
		}}),
		confii.WithEnvExpander(false),
		confii.WithTypeCasting(false),
	)
	require.NoError(t, err)

	got, err := cfg.Get("value")
	require.NoError(t, err)
	assert.Equal(t, "${cmd:printf nope}", got)
}
