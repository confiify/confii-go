// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	confii "github.com/confiify/confii-go/v2"
	"github.com/confiify/confii-go/v2/loader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnChange_FiresOnTypeDrift_IntToFloat(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	require.NoError(t, cfg.Set("port", 8080))

	var fired atomic.Int32
	var oldType, newType string
	var mu sync.Mutex
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if key != "port" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		fired.Add(1)

		switch oldVal.(type) {
		case int:
			oldType = "int"
		case float64:
			oldType = "float64"
		default:
			oldType = "other"
		}
		switch newVal.(type) {
		case int:
			newType = "int"
		case float64:
			newType = "float64"
		default:
			newType = "other"
		}
	})

	require.NoError(t, cfg.Set("port", float64(8080)))

	if fired.Load() == 0 {
		t.Fatalf(":type drift int(8080) -> float64(8080) must fire OnChange (diff was suppressed pre-)")
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "int", oldType, ":oldVal must be the original int")
	assert.Equal(t, "float64", newType, ":newVal must be the new float64")
}

func TestOnChange_NoopSet_DoesNotFire(t *testing.T) {
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)
	require.NoError(t, cfg.Set("port", 8080))

	var fired atomic.Int32
	cfg.OnChange(func(key string, oldVal, newVal any) {
		if key == "port" {
			fired.Add(1)
		}
	})

	require.NoError(t, cfg.Set("port", 8080))
	assert.Equal(t, int32(0), fired.Load(),
		":idempotent Set with identical type+value must not fire OnChange")
}

func TestEnableVersioningAfterSaveVersion_PathTakesEffect(t *testing.T) {
	dir := t.TempDir()
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	v1, err := cfg.SaveVersion(map[string]any{"trigger": "boot"})
	require.NoError(t, err)
	require.NotNil(t, v1)

	cfg.EnableVersioning(filepath.Join(dir, "versions"), 50)

	v2, err := cfg.SaveVersion(map[string]any{"trigger": "post-enable"})
	require.NoError(t, err)
	require.NotNil(t, v2)

	persisted := filepath.Join(dir, "versions", v2.VersionID+".json")
	if _, err := os.Stat(persisted); err != nil {
		t.Fatalf(":expected persisted snapshot at %s, got %v (EnableVersioning args were silently dropped)", persisted, err)
	}
}

func TestConcurrentSaveVersionAndEnableVersioning(t *testing.T) {
	dir := t.TempDir()
	cfg, err := confii.NewWithContext[any](context.Background(),
		confii.WithLoaders(loader.NewYAML("loader/testdata/simple.yaml")),
	)
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = cfg.SaveVersion(map[string]any{"trigger": "concurrent-save"})
	}()
	go func() {
		defer wg.Done()
		cfg.EnableVersioning(filepath.Join(dir, "versions"), 25)
	}()
	wg.Wait()

	v, err := cfg.SaveVersion(map[string]any{"trigger": "after-race"})
	require.NoError(t, err)
	persisted := filepath.Join(dir, "versions", v.VersionID+".json")
	if _, err := os.Stat(persisted); err != nil {
		t.Fatalf(":post-race SaveVersion must persist; missing file %s:%v", persisted, err)
	}
}

func TestDictProvider_Registered(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("dict")
	require.True(t, ok, ":dict provider must be registered")
	require.NotNil(t, factory)

	store, err := factory(context.Background(), map[string]any{
		"entries": map[string]any{
			"db_password": "s3cr3t",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	got, err := store.ReadSecret(context.Background(), confii.SecretRequest{Key: "db_password"})
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t", got)
	_, err = store.ReadSecret(context.Background(), confii.SecretRequest{Key: "missing"})
	require.ErrorIs(t, err, confii.ErrSecretNotFound)
}

func TestDictProvider_InputShapes(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("dict")
	require.True(t, ok)

	empty, err := factory(context.Background(), map[string]any{})
	require.NoError(t, err)
	_, err = empty.ReadSecret(context.Background(), confii.SecretRequest{Key: "missing"})
	require.ErrorIs(t, err, confii.ErrSecretNotFound)

	converted, err := factory(context.Background(), map[string]any{"entries": map[any]any{"token": "value"}})
	require.NoError(t, err)
	got, err := converted.ReadSecret(context.Background(), confii.SecretRequest{Key: "token"})
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	_, err = factory(context.Background(), map[string]any{"entries": map[any]any{42: "bad"}})
	require.Error(t, err)
	_, err = factory(context.Background(), map[string]any{"entries": "not-a-map"})
	require.Error(t, err)
}

func TestFileProvider_Registered(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok, ":file provider must be registered")
	require.NotNil(t, factory)

	tmp := t.TempDir()
	store, err := factory(context.Background(), map[string]any{
		"base_dir": tmp,
	})
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestFileProvider_ReadOptionsAndErrors(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok)

	_, err := factory(context.Background(), map[string]any{})
	require.Error(t, err)

	base := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(base, "api.key"), []byte("  secret-value\n"), 0600))
	trimmed, err := factory(context.Background(), map[string]any{"base_dir": base, "extension": ".key"})
	require.NoError(t, err)
	got, err := trimmed.ReadSecret(context.Background(), confii.SecretRequest{Key: "api"})
	require.NoError(t, err)
	assert.Equal(t, "secret-value", got)

	untrimmed, err := factory(context.Background(), map[string]any{
		"base_dir":        base,
		"extension":       ".key",
		"trim_whitespace": false,
	})
	require.NoError(t, err)
	got, err = untrimmed.ReadSecret(context.Background(), confii.SecretRequest{Key: "api"})
	require.NoError(t, err)
	assert.Equal(t, "  secret-value\n", got)

	_, err = trimmed.ReadSecret(context.Background(), confii.SecretRequest{Key: "missing"})
	require.ErrorIs(t, err, confii.ErrSecretNotFound)
	_, err = trimmed.ReadSecret(context.Background(), confii.SecretRequest{Key: "bad\x00key"})
	require.ErrorIs(t, err, confii.ErrSecretNotFound)

	missingRoot, err := factory(context.Background(), map[string]any{"base_dir": filepath.Join(base, "absent")})
	require.NoError(t, err)
	_, err = missingRoot.ReadSecret(context.Background(), confii.SecretRequest{Key: "api"})
	require.Error(t, err)
}

func TestEnvProvider_StillRegistered(t *testing.T) {
	_, ok := confii.LookupSelfConfigSecretProvider("env")
	require.True(t, ok, ":env provider must remain registered")
}

func TestUnknownProvider_TypedErrorListsRegisteredNames(t *testing.T) {
	_, ok := confii.LookupSelfConfigSecretProvider("nonexistent_provider_v24")
	require.False(t, ok, ":unknown provider must not resolve")
}

func TestRegisterCustomProvider_Works(t *testing.T) {
	confii.RegisterSelfConfigSecretProvider("v24-test-provider", func(_ context.Context, cfg map[string]any) (confii.SecretReader, error) {
		return v24FakeStore{}, nil
	})

	factory, ok := confii.LookupSelfConfigSecretProvider("v24-test-provider")
	require.True(t, ok, ":custom provider must register")
	require.NotNil(t, factory)

	store, err := factory(context.Background(), map[string]any{})
	require.NoError(t, err)
	got, err := store.ReadSecret(context.Background(), confii.SecretRequest{Key: "any-key"})
	require.NoError(t, err)
	assert.Equal(t, "v24-fake", got)
}

type v24FakeStore struct{}

func (v24FakeStore) ReadSecret(_ context.Context, _ confii.SecretRequest) (any, error) {
	return "v24-fake", nil
}

func TestFileProvider_RejectsPathTraversal(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok)
	tmp := t.TempDir()
	store, err := factory(context.Background(), map[string]any{"base_dir": tmp})
	require.NoError(t, err)

	for _, badKey := range []string{
		"../etc/passwd",
		"../../root/.ssh/id_rsa",
		"/etc/shadow",
		"valid_prefix/../etc/passwd",
	} {
		_, err := store.ReadSecret(context.Background(), confii.SecretRequest{Key: badKey})
		if err == nil {
			t.Errorf(":file provider must reject path-traversal key %q", badKey)
		}
	}
}

func TestFileProvider_RejectsSymlinkEscape(t *testing.T) {
	factory, ok := confii.LookupSelfConfigSecretProvider("file")
	require.True(t, ok)
	base := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("escaped"), 0600))
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	store, err := factory(context.Background(), map[string]any{"base_dir": base})
	require.NoError(t, err)
	_, err = store.ReadSecret(context.Background(), confii.SecretRequest{Key: "escape/secret"})
	require.Error(t, err)
}
